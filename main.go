package main

import (
	"embed"
	_ "embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Album struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title"`
}

type Photo struct {
	ID      uint   `json:"id" gorm:"primaryKey"`
	AlbumID uint   `json:"albumId"`
	Title   string `json:"title"`
	URL     string `json:"url"`
}

//go:embed database.db
var embeddedDB []byte

//go:embed uploads/*
var embeddedUploads embed.FS

var db *gorm.DB

func initDB1() {
	var err error
	db, err = gorm.Open(sqlite.Open("database.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("error db:", err)
	}
	db.AutoMigrate(&Album{}, &Photo{})
}

func initDB() {
	configDir := os.TempDir()
	var err error

	dbPath := filepath.Join(configDir, "album-server", "database.db")

	if _, err := os.Stat(filepath.Dir(dbPath)); os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(dbPath), os.ModePerm)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		err := os.WriteFile(dbPath, embeddedDB, 0644)
		if err != nil {
			log.Fatal("Ошибка при копировании БД:", err)
		}
		fmt.Println("База данных извлечена в:", dbPath)
	}

	db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}

	db.AutoMigrate(&Album{}, &Photo{})
}

func restoreUploads() {
	tempDir := os.TempDir()
	uploadsDir := filepath.Join(tempDir, "album-server", "uploads")

	if _, err := os.Stat(uploadsDir); os.IsNotExist(err) {
		os.MkdirAll(uploadsDir, os.ModePerm)
	}

	files, err := fs.ReadDir(embeddedUploads, "uploads")
	if err != nil {
		log.Fatal("Ошибка при чтении встроенных файлов:", err)
	}

	for _, file := range files {
		dstPath := filepath.Join(uploadsDir, file.Name())

		if _, err := os.Stat(dstPath); os.IsNotExist(err) {
			data, err := embeddedUploads.ReadFile("uploads/" + file.Name())
			if err != nil {
				log.Println("Ошибка при извлечении файла:", file.Name(), err)
				continue
			}
			err = os.WriteFile(dstPath, data, 0644)
			if err != nil {
				log.Println("Ошибка при сохранении файла:", dstPath, err)
			} else {
				fmt.Println("Файл извлечён во временную папку:", dstPath)
			}
		}
	}

	uploadsPath = uploadsDir
}

var uploadsPath string

func main() {
	initDB()
	restoreUploads()
	if _, err := os.Stat("uploads"); os.IsNotExist(err) {
		os.Mkdir("uploads", os.ModePerm)
	}

	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))
	r.Static("/uploads", uploadsPath)

	r.GET("/albums", getAlbums)
	r.GET("/albums/:id", getAlbumByID)
	r.PUT("/albums/:id", updateAlbum)
	r.POST("/albums", createAlbum)
	r.GET("/albums/:id/photos", getPhotosByAlbum)
	r.POST("/albums/:id/photos", uploadPhoto)
	r.DELETE("/albums/:id", deleteAlbum)
	r.Run(":8080")
}

func getAlbumByID(c *gin.Context) {
	albumID, _ := strconv.Atoi(c.Param("id"))

	var album Album
	result := db.First(&album, albumID)

	if result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Album not found"})
		return
	}

	c.JSON(http.StatusOK, album)
}

func updateAlbum(c *gin.Context) {
	albumID, _ := strconv.Atoi(c.Param("id"))

	var album Album
	result := db.First(&album, albumID)
	if result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Album not found"})
		return
	}

	var updatedData struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&updatedData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	album.Title = updatedData.Title
	db.Save(&album)

	c.JSON(http.StatusOK, album)
}

func deleteAlbum(c *gin.Context) {
	albumID, _ := strconv.Atoi(c.Param("id"))

	db.Where("album_id = ?", albumID).Delete(&Photo{})

	result := db.Delete(&Album{}, albumID)

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "Album not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Album deleted"})
}

func getAlbums(c *gin.Context) {
	var albums []Album
	db.Find(&albums)
	c.JSON(http.StatusOK, albums)
}

func createAlbum(c *gin.Context) {
	var newAlbum Album
	if err := c.ShouldBindJSON(&newAlbum); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	db.Create(&newAlbum)
	c.JSON(http.StatusCreated, newAlbum)
}

func getPhotosByAlbum(c *gin.Context) {
	albumID, _ := strconv.Atoi(c.Param("id"))
	var photos []Photo
	db.Where("album_id = ?", albumID).Find(&photos)
	c.JSON(http.StatusOK, photos)
}

func uploadPhoto(c *gin.Context) {
	albumID, _ := strconv.Atoi(c.Param("id"))

	file, err := c.FormFile("photo")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Файл обязателен"})
		return
	}

	filename := fmt.Sprintf("uploads/%d_%s", albumID, filepath.Base(file.Filename))
	if err := c.SaveUploadedFile(file, filename); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения файла"})
		return
	}

	photo := Photo{
		AlbumID: uint(albumID),
		Title:   file.Filename,
		URL:     "/" + filename,
	}
	db.Create(&photo)

	c.JSON(http.StatusCreated, photo)
}

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/gcs"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
)

func main() {
	// 1. Load Config
	config.LoadConfig()

	// 2. Connect DB (which runs AutoMigrate and adds "tamano" column)
	db.ConnectDB()

	// 3. Initialize GCS Client
	if err := gcs.InitGCS(); err != nil {
		log.Fatalf("Error al inicializar GCS: %v", err)
	}

	ctx := context.Background()

	// 4. Migrate documentos
	var documentos []models.Documento
	if err := db.DB.Where("tamano = 0").Find(&documentos).Error; err != nil {
		log.Fatalf("Error al obtener documentos de la BD: %v", err)
	}

	fmt.Printf("Migrando %d documentos activos...\n", len(documentos))
	migratedDocs := 0
	for _, doc := range documentos {
		if doc.FilePath == "" {
			continue
		}
		size, err := gcs.ObtenerTamanoArchivo(ctx, doc.FilePath)
		if err != nil {
			fmt.Printf("[WARN] No se pudo obtener tamaño para documento %d (GCS: %s): %v\n", doc.ID, doc.FilePath, err)
			continue
		}

		if err := db.DB.Model(&doc).Update("tamano", size).Error; err != nil {
			fmt.Printf("[ERROR] No se pudo actualizar tamaño para documento %d: %v\n", doc.ID, err)
		} else {
			migratedDocs++
		}
	}
	fmt.Printf("Migración de documentos completada. Total migrados con éxito: %d\n", migratedDocs)

	// 5. Migrate documentos_eliminados
	var docsEliminados []models.DocumentoEliminado
	if err := db.DB.Where("tamano = 0").Find(&docsEliminados).Error; err != nil {
		log.Fatalf("Error al obtener documentos eliminados de la BD: %v", err)
	}

	fmt.Printf("Migrando %d registros de papelera...\n", len(docsEliminados))
	migratedTrash := 0
	for _, docTrash := range docsEliminados {
		if docTrash.FilePathPapelera == "" {
			continue
		}
		size, err := gcs.ObtenerTamanoArchivo(ctx, docTrash.FilePathPapelera)
		if err != nil {
			fmt.Printf("[WARN] No se pudo obtener tamaño para papelera %d (GCS: %s): %v\n", docTrash.ID, docTrash.FilePathPapelera, err)
			continue
		}

		if err := db.DB.Model(&docTrash).Update("tamano", size).Error; err != nil {
			fmt.Printf("[ERROR] No se pudo actualizar tamaño para papelera %d: %v\n", docTrash.ID, err)
		} else {
			migratedTrash++
		}
	}
	fmt.Printf("Migración de papelera completada. Total migrados con éxito: %d\n", migratedTrash)
}

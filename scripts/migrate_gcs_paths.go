package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/gcs"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
)

func main() {
	fmt.Println("🚀 Iniciando migración de rutas GCS segura (Copiando archivos: sysdocpruebas -> App_Documentos)...")

	// 1. Cargar configuración
	config.LoadConfig()

	// 2. Conectar a Base de Datos
	db.ConnectDB()

	// 3. Inicializar GCS
	if err := gcs.InitGCS(); err != nil {
		log.Fatalf("❌ Error crítico inicializando GCS: %v", err)
	}

	ctx := context.Background()

	// ==========================================
	// 4. Migración de Documentos Activos
	// ==========================================
	fmt.Println("\n📂 Procesando tabla 'documentos'...")
	var documentos []models.Documento
	if err := db.DB.Where("file_path LIKE ?", "sysdocpruebas/%").Find(&documentos).Error; err != nil {
		log.Fatalf("Error consultando documentos: %v", err)
	}

	fmt.Printf("Se encontraron %d documentos activos en la ruta de pruebas.\n", len(documentos))
	successDocCount := 0

	for _, doc := range documentos {
		oldPath := doc.FilePath
		newPath := strings.Replace(oldPath, "sysdocpruebas", "App_Documentos", 1)

		fmt.Printf("👉 Copiando documento ID %d en GCS:\n   Origen:  %s\n   Destino: %s\n", doc.ID, oldPath, newPath)

		// Copiar archivo en GCS (Mantiene el origen por seguridad)
		err := gcs.CopiarArchivo(ctx, oldPath, newPath)
		if err != nil {
			fmt.Printf("⚠️ [WARN] Error al copiar en GCS: %v. Se actualizará la base de datos de igual forma por si el archivo ya fue movido o copiado manualmente.\n", err)
		}

		// Actualizar registro en DB
		if err := db.DB.Model(&doc).Update("file_path", newPath).Error; err != nil {
			fmt.Printf("❌ [ERROR] No se pudo actualizar el registro DB para el documento %d: %v\n", doc.ID, err)
		} else {
			fmt.Printf("✅ [SUCCESS] Registro DB del Documento ID %d actualizado a la nueva ruta.\n", doc.ID)
			successDocCount++
		}
	}

	// ==========================================
	// 5. Migración de Documentos Eliminados (Papelera)
	// ==========================================
	fmt.Println("\n🗑️ Procesando tabla 'documentos_eliminados'...")
	var eliminados []models.DocumentoEliminado
	if err := db.DB.Where("file_path_original LIKE ? OR file_path_papelera LIKE ?", "sysdocpruebas/%", "sysdocpruebas/%").Find(&eliminados).Error; err != nil {
		log.Fatalf("Error consultando documentos eliminados: %v", err)
	}

	fmt.Printf("Se encontraron %d documentos eliminados/papelera con rutas de pruebas.\n", len(eliminados))
	successDelCount := 0

	for _, del := range eliminados {
		updates := make(map[string]interface{})

		// Actualizar metadata de ruta original si contenía la ruta de pruebas
		if strings.HasPrefix(del.FilePathOriginal, "sysdocpruebas/") {
			newOrig := strings.Replace(del.FilePathOriginal, "sysdocpruebas", "App_Documentos", 1)
			updates["file_path_original"] = newOrig
		}

		// Si el archivo físico en la papelera tiene el prefijo de pruebas, copiarlo
		if strings.HasPrefix(del.FilePathPapelera, "sysdocpruebas/") {
			oldTrash := del.FilePathPapelera
			newTrash := strings.Replace(oldTrash, "sysdocpruebas", "App_Documentos", 1)

			fmt.Printf("👉 Copiando archivo de papelera ID %d en GCS:\n   Origen:  %s\n   Destino: %s\n", del.ID, oldTrash, newTrash)

			err := gcs.CopiarArchivo(ctx, oldTrash, newTrash)
			if err != nil {
				fmt.Printf("⚠️ [WARN] Error al copiar archivo de papelera en GCS: %v. Se actualizará la base de datos igualmente.\n", err)
			}
			updates["file_path_papelera"] = newTrash
		}

		if len(updates) > 0 {
			if err := db.DB.Model(&del).Updates(updates).Error; err != nil {
				fmt.Printf("❌ [ERROR] No se pudo actualizar el registro de papelera ID %d: %v\n", del.ID, err)
			} else {
				fmt.Printf("✅ [SUCCESS] Registro DB de papelera ID %d actualizado.\n", del.ID)
				successDelCount++
			}
		}
	}

	fmt.Println("\n🏁 ========================================================")
	fmt.Printf("🎉 Migración Segura de Copiado Finalizada.\n")
	fmt.Printf("   - Documentos Activos Copiados/Migrados: %d/%d\n", successDocCount, len(documentos))
	fmt.Printf("   - Registros de Papelera Copiados/Migrados: %d/%d\n", successDelCount, len(eliminados))
	fmt.Println("\n⚠️  [IMPORTANTE]: Los archivos originales aún se conservan en la carpeta 'sysdocpruebas' de tu bucket como respaldo.")
	fmt.Println("   Una vez verifiques que el sistema funciona correctamente y accedes a los archivos sin problemas,")
	fmt.Println("   puedes borrar la carpeta de pruebas ('sysdocpruebas') directamente desde la consola web de Google Cloud Storage.")
	fmt.Println("============================================================")
}

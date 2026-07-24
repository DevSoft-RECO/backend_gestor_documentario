package db

import (
	"fmt"
	"log"
	"strings"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectDB() {
	dbURL := config.Envs.DBURL
	driver := strings.ToLower(strings.TrimSpace(config.Envs.DBDriver))

	if strings.TrimSpace(dbURL) == "" {
		log.Fatalf("DATABASE_URL vacío o no definido")
	}

	// Fallback: infer from DATABASE_URL prefix if DB_DRIVER not set.
	if driver == "" {
		switch {
		case strings.HasPrefix(dbURL, "postgres://") || strings.HasPrefix(dbURL, "postgresql://") || strings.HasPrefix(dbURL, "postgres+psycopg2://"):
			driver = "postgres"
		case strings.HasPrefix(dbURL, "mysql://") || strings.HasPrefix(dbURL, "mysql+pymysql://"):
			driver = "mysql"
		default:
			// Keep mysql as historical default for this template.
			driver = "mysql"
		}
	}

	var (
		err error
		dsn string
	)

	switch driver {
	case "postgres", "postgresql":
		dsn = config.Envs.GetPostgresDSN()
		DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	case "mysql":
		dsn = config.Envs.GetMySQLDSN()
		DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	default:
		log.Fatalf("DB_DRIVER inválido: %q (usa mysql o postgres)", config.Envs.DBDriver)
	}
	if err != nil {
		log.Fatalf("Error conectando a la base de datos (%s): %v", driver, err)
	}

	// Auto-migración
	if err := DB.AutoMigrate(&models.Agencia{}, &models.Usuario{}, &models.Puesto{}, &models.Categoria{}, &models.Subcategoria{}, &models.Asociado{}, &models.Documento{}, &models.IndicePagina{}, &models.ManualCategoria{}, &models.ManualSubcategoria{}, &models.ManualCarpeta{}, &models.ManualDocumento{}, &models.ManualActualizacion{}, &models.DocumentoEliminado{}, &models.SubcategoriaPuesto{}); err != nil {
		log.Printf("[ERROR] Error en auto-migración: %v", err)
	}

	// Migración JIT para inicializar permisos existentes a true/true
	if err := DB.Exec("UPDATE subcategoria_puestos SET ver = true, editar = true WHERE ver = false AND editar = false").Error; err != nil {
		log.Printf("[DB-WARN] No se pudo inicializar ver/editar en subcategoria_puestos: %v", err)
	}
	
	// Quitar el NOT NULL de la columna obsoleta manual_subcategoria_id en manual_documentos
	if err := DB.Exec("ALTER TABLE manual_documentos ALTER COLUMN manual_subcategoria_id DROP NOT NULL").Error; err != nil {
		log.Printf("[DB-WARN] No se pudo alterar columna obsoleta manual_subcategoria_id: %v", err)
	}

	fmt.Printf("Conexión a la base de datos establecida correctamente (%s).\n", driver)
}

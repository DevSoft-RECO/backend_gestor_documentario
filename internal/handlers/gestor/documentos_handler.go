package gestor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// ObtenerExpediente devuelve todos los documentos de un asociado,
// precargando la información de sus subcategorías y categorías.
func ObtenerExpediente(c *fiber.Ctx) error {
	asociadoID := c.Params("asociado_id")

	var documentos []models.Documento
	err := db.DB.Preload("Subcategoria").
		Preload("Subcategoria.Categoria", func(db *gorm.DB) *gorm.DB {
			return db.Select("ID, Nombre")
		}).
		Where("asociado_id = ?", asociadoID).
		Find(&documentos).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener expediente"})
	}

	return c.JSON(documentos)
}

// SubirDocumento maneja la carga física del PDF y registra el puente en la BD.
func SubirDocumento(c *fiber.Ctx) error {
	asociadoIDStr := c.FormValue("asociado_id")
	subcategoriaIDStr := c.FormValue("subcategoria_id")

	if asociadoIDStr == "" || subcategoriaIDStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Faltan parámetros requeridos"})
	}

	asociadoID, err := strconv.ParseUint(asociadoIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de asociado inválido"})
	}

	subcategoriaID, err := strconv.ParseUint(subcategoriaIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de subcategoría inválido"})
	}

	file, err := c.FormFile("documento")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No se recibió ningún archivo o es inválido"})
	}

	// Obtener información de la subcategoría para nombrar el archivo
	var subcategoria models.Subcategoria
	if err := db.DB.First(&subcategoria, subcategoriaID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Subcategoría no encontrada"})
	}

	// Crear el directorio específico para este asociado si no existe
	baseUploadPath := "./uploads/expedientes"
	asociadoDir := filepath.Join(baseUploadPath, fmt.Sprintf("asociado_%d", asociadoID))
	if err := os.MkdirAll(asociadoDir, os.ModePerm); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear directorio en el servidor"})
	}

	// Limpiar el nombre de la subcategoría para usarlo como nombre de archivo
	// En un entorno de producción, sería mejor una función de sanitización completa.
	// Por simplicidad, usamos subcategoriaID y timestamp.
	fileName := fmt.Sprintf("doc_subcat_%d_%d.pdf", subcategoriaID, time.Now().Unix())
	filePath := filepath.Join(asociadoDir, fileName)

	// Guardar el archivo físico
	if err := c.SaveFile(file, filePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar el archivo físico"})
	}

	// Ruta relativa para servir mediante HTTP (reemplazando barras invertidas de Windows si las hay)
	httpPath := fmt.Sprintf("/uploads/expedientes/asociado_%d/%s", asociadoID, fileName)

	// Extraer UsuarioID del token
	var usuarioID uint = 1 // Fallback
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		if sub, ok := claims["sub"]; ok {
			if idFloat, ok := sub.(float64); ok {
				usuarioID = uint(idFloat)
			} else if idStr, ok := sub.(string); ok {
				if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
					usuarioID = uint(parsed)
				}
			}
		}
	}

	// Buscar si ya existe un documento para esta combinación Asociado+Subcategoría
	var documento models.Documento
	result := db.DB.Where("asociado_id = ? AND subcategoria_id = ?", asociadoID, subcategoriaID).First(&documento)

	if result.Error == nil {
		// Ya existe: actualizar la ruta y el timestamp
		// (Opcional: eliminar el archivo físico anterior del disco aquí)
		documento.FilePath = httpPath
		if err := db.DB.Save(&documento).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar registro de documento"})
		}
	} else {
		// No existe: crearlo nuevo
		documento = models.Documento{
			AsociadoID:     uint(asociadoID),
			SubcategoriaID: uint(subcategoriaID),
			FilePath:       httpPath,
			UsuarioID:      usuarioID,
		}
		if err := db.DB.Create(&documento).Error; err != nil {
			fmt.Printf("[ERROR] Falló al crear documento en BD: %v\n", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear registro de documento", "detalle": err.Error()})
		}
	}

	// Crear índice inicial si se proporciona etiqueta
	etiqueta := c.FormValue("etiqueta")
	if etiqueta != "" {
		fechaVencimiento := c.FormValue("fecha_vencimiento")
		var fechaVencimientoPtr *time.Time
		if fechaVencimiento != "" {
			if parsedDate, err := time.Parse("2006-01-02", fechaVencimiento); err == nil {
				fechaVencimientoPtr = &parsedDate
			}
		}

		// Limpiar posible índice anterior en la página 1 si se está sobreescribiendo el fólder
		db.DB.Where("documento_id = ? AND pagina_inicio = 1", documento.ID).Delete(&models.IndicePagina{})

		numeroDocumento := c.FormValue("numero_documento")
		var numDocPtr *string
		if numeroDocumento != "" {
			numDocPtr = &numeroDocumento
		}

		indice := models.IndicePagina{
			DocumentoID:      documento.ID,
			PaginaInicio:     1,
			TipoMovimiento:   "Documento Maestro",
			Etiqueta:         etiqueta,
			NumeroDocumento:  numDocPtr,
			UsuarioID:        usuarioID,
			FechaVencimiento: fechaVencimientoPtr,
		}
		db.DB.Create(&indice)
	}

	// Devolver el documento creado/actualizado
	// Precargamos la subcategoría para enviarla al frontend
	db.DB.Preload("Subcategoria").First(&documento, documento.ID)

	return c.Status(fiber.StatusOK).JSON(documento)
}

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
		Preload("Subcategoria.PuestosAutorizados").
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

	// Obtener información de la subcategoría para nombrar el archivo y verificar permisos
	var subcategoria models.Subcategoria
	if err := db.DB.Preload("PuestosAutorizados").First(&subcategoria, subcategoriaID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Subcategoría no encontrada"})
	}

	// === VERIFICACIÓN DE PERMISOS POR PUESTO ===
	// 1. Obtener el usuario actual con su puesto y roles
	var usuarioID uint = 1 // Fallback
	isSuperAdmin := false
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		// Verificar si es Super Admin
		if rolesRaw, ok := claims["roles"]; ok {
			if roles, ok := rolesRaw.([]interface{}); ok {
				for _, r := range roles {
					if r == "Super Admin" || r == "Administrador" { // Ajustar según nombres reales
						isSuperAdmin = true
						break
					}
				}
			}
		}

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

	if !isSuperAdmin {
		var userLocal models.Usuario
		if err := db.DB.Preload("Puesto").First(&userLocal, usuarioID).Error; err == nil {
			// Si la subcategoría tiene restricciones de puestos
			if len(subcategoria.PuestosAutorizados) > 0 {
				authorized := false
				if userLocal.IDPuesto != nil {
					for _, p := range subcategoria.PuestosAutorizados {
						if p.ID == *userLocal.IDPuesto {
							authorized = true
							break
						}
					}
				}
				if !authorized {
					return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
						"error":   "No tienes permiso para crear carpetas/documentos en esta subcategoría",
						"detalle": "Tu puesto no está autorizado para esta categoría.",
					})
				}
			}
		}
	}
	// === FIN VERIFICACIÓN DE PERMISOS ===

	// Obtener el asociado para extraer su código de cliente
	var asociado models.Asociado
	if err := db.DB.First(&asociado, asociadoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Asociado no encontrado"})
	}

	// Crear el directorio específico para este asociado si no existe
	baseUploadPath := "./uploads/expedientes"
	asociadoDir := filepath.Join(baseUploadPath, fmt.Sprintf("asociado_%d", asociadoID))
	if err := os.MkdirAll(asociadoDir, os.ModePerm); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear directorio en el servidor"})
	}

	// Usar el Código de Cliente para el nombre del archivo. Fallback a DPI o ID.
	identificador := ""
	if asociado.CodigoCliente != nil && *asociado.CodigoCliente != "" {
		identificador = *asociado.CodigoCliente
	} else if asociado.DPI != "" {
		identificador = asociado.DPI
	} else {
		identificador = fmt.Sprintf("id_%d", asociadoID)
	}

	fileName := fmt.Sprintf("doc_subcat_%d_%s.pdf", subcategoriaID, identificador)
	filePath := filepath.Join(asociadoDir, fileName)

	// Guardar el archivo físico
	if err := c.SaveFile(file, filePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar el archivo físico"})
	}

	// Ruta relativa para servir mediante HTTP (reemplazando barras invertidas de Windows si las hay)
	httpPath := fmt.Sprintf("/uploads/expedientes/asociado_%d/%s", asociadoID, fileName)

	// Extraer UsuarioID del token (ya extraído arriba en la verificación de permisos)

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

		// Como se sobreescribió todo el PDF físico, debemos eliminar todos los índices lógicos anteriores
		// para evitar desincronizaciones con las páginas del nuevo documento.
		db.DB.Where("documento_id = ?", documento.ID).Delete(&models.IndicePagina{})
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

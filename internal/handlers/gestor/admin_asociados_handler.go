package gestor

import (
	"context"
	"strconv"
	"strings"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/gcs"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// Helper interno para verificar si es Super Admin de forma óptima y robusta
func isUserSuperAdmin(c *fiber.Ctx) bool {
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	if !ok {
		return false
	}

	// 1. Verificar roles en Claims (Token)
	if rolesRaw, ok := claims["roles"]; ok {
		switch r := rolesRaw.(type) {
		case []interface{}:
			for _, role := range r {
				if s, ok := role.(string); ok && s == "Super Admin" {
					return true
				}
			}
		case string:
			if r == "Super Admin" {
				return true
			}
		}
	}

	// 2. Consulta de respaldo rápida a la Base de Datos Local
	var usuarioID uint
	if sub, ok := claims["sub"]; ok {
		if idFloat, ok := sub.(float64); ok {
			usuarioID = uint(idFloat)
		} else if idStr, ok := sub.(string); ok {
			if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
				usuarioID = uint(parsed)
			}
		}
	}

	if usuarioID == 0 {
		return false
	}

	var userLocal models.Usuario
	// Seleccionamos exclusivamente la columna roles para máxima eficiencia
	if err := db.DB.Select("roles").First(&userLocal, usuarioID).Error; err == nil {
		if userLocal.Roles != nil {
			if strings.Contains(*userLocal.Roles, "Super Admin") {
				return true
			}
		}
	}

	return false
}

type AdminAsociadoResponse struct {
	models.Asociado
	TotalDocumentos int64 `json:"total_documentos"`
}

func GetAdminAsociados(c *fiber.Ctx) error {
	if !isUserSuperAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Acceso restringido a Super Administradores"})
	}

	pageStr := c.Query("page", "1")
	limitStr := c.Query("limit", "10")
	search := c.Query("search", "")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 10
	}

	offset := (page - 1) * limit

	var total int64
	query := db.DB.Model(&models.Asociado{})

	if search != "" {
		searchPattern := "%" + search + "%"
		query = query.Where("nombre_completo LIKE ? OR dpi LIKE ? OR codigo_cliente LIKE ?", searchPattern, searchPattern, searchPattern)
	}

	if err := query.Count(&total).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al contar asociados"})
	}

	var asociados []AdminAsociadoResponse
	err = query.
		Select("asociados.*, (SELECT COUNT(*) FROM documentos WHERE documentos.asociado_id = asociados.id) as total_documentos").
		Order("nombre_completo ASC").
		Limit(limit).
		Offset(offset).
		Scan(&asociados).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al recuperar asociados"})
	}

	return c.JSON(fiber.Map{
		"asociados": asociados,
		"total":     total,
		"page":      page,
		"limit":     limit,
	})
}

// DeleteAdminAsociado elimina a un asociado, su portafolio, sus documentos e índices vinculados y borra archivos en GCS
func DeleteAdminAsociado(c *fiber.Ctx) error {
	if !isUserSuperAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Acceso restringido a Super Administradores"})
	}

	id := c.Params("id")

	// 1. Buscar los documentos vinculados para limpiar GCS posteriormente
	var documentos []models.Documento
	if err := db.DB.Where("asociado_id = ?", id).Find(&documentos).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al buscar documentos asociados"})
	}

	// 2. Iniciar una transacción de base de datos para asegurar atomicidad
	tx := db.DB.Begin()
	if tx.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al iniciar la transacción"})
	}

	// 3. Eliminar en cascada
	for _, doc := range documentos {
		// Eliminar índices vinculados al documento
		if err := tx.Where("documento_id = ?", doc.ID).Delete(&models.IndicePagina{}).Error; err != nil {
			tx.Rollback()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar índices de páginas"})
		}
	}

	// Eliminar todos los registros de documentos del asociado
	if err := tx.Where("asociado_id = ?", id).Delete(&models.Documento{}).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar registros de documentos"})
	}

	// Eliminar el asociado en sí
	if err := tx.Delete(&models.Asociado{}, id).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar asociado"})
	}

	// Confirmar los cambios en base de datos
	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al confirmar la eliminación"})
	}

	// 4. Eliminar físicamente los archivos de GCS en segundo plano
	go func(docs []models.Documento) {
		ctx := context.Background()
		for _, doc := range docs {
			if doc.FilePath != "" {
				_ = gcs.EliminarArchivo(ctx, doc.FilePath)
			}
		}
	}(documentos)

	return c.JSON(fiber.Map{
		"status": "ok",
		"mensaje": "Asociado, portafolio e índices eliminados correctamente del ecosistema.",
	})
}

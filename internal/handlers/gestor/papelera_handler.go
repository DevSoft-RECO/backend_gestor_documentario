package gestor

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/gcs"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// EliminarDocumentoPapelera realiza un soft delete moviendo el archivo en GCS a la papelera y registrándolo en documentos_eliminados
// DELETE /api/gestor/documentos/:documento_id/eliminar-papelera
func EliminarDocumentoPapelera(c *fiber.Ctx) error {
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	var usuarioID uint = 1
	if ok {
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

	docIDStr := c.Params("documento_id")
	docID, err := strconv.ParseUint(docIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	// 1. Buscar el documento completo con relaciones para guardar datos estáticos
	var documento models.Documento
	if err := db.DB.Preload("Subcategoria.Categoria").Preload("Asociado").First(&documento, docID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado"})
	}

	// Generar nueva ruta de papelera con timestamp
	timestamp := time.Now().Unix()
	ext := ".pdf" // Por defecto son PDFs en este gestor
	
	// Extraer nombre base o generar ruta nueva en papelera
	fileName := fmt.Sprintf("doc_subcat_%d_%d_%d%s", documento.SubcategoriaID, documento.AsociadoID, timestamp, ext)
	gcsTrashPath := fmt.Sprintf("%s/papelera/asociado_%d/%s", config.Envs.GCSPathPrefix, documento.AsociadoID, fileName)

	// 2. Mover el archivo físico en GCS
	if documento.FilePath != "" {
		ctx := c.UserContext()
		if err := gcs.MoverArchivo(ctx, documento.FilePath, gcsTrashPath); err != nil {
			// Si no se puede mover, tal vez el archivo no existe en GCS, pero registramos la advertencia e intentamos continuar
			fmt.Printf("[WARN] No se pudo mover el archivo físico de %s a %s en GCS: %v\n", documento.FilePath, gcsTrashPath, err)
			gcsTrashPath = documento.FilePath // fallback a la ruta original
		}
	}

	// 3. Transacción de base de datos
	tx := db.DB.Begin()
	if tx.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al iniciar la transacción"})
	}

	// Registrar en documentos_eliminados
	docEliminado := models.DocumentoEliminado{
		DocumentoIDOriginal: documento.ID,
		AsociadoID:          documento.AsociadoID,
		SubcategoriaID:      documento.SubcategoriaID,
		NombreSubcategoria:  documento.Subcategoria.Nombre,
		NombreCategoria:     documento.Subcategoria.Categoria.Nombre,
		NombreAsociado:      documento.Asociado.NombreCompleto,
		FilePathOriginal:    documento.FilePath,
		FilePathPapelera:    gcsTrashPath,
		TotalPaginas:        documento.TotalPaginas,
		Tamano:              documento.Tamano,
		UsuarioEliminoID:    usuarioID,
		UsuarioAsignadoID:   nil, // Va a buzón general al inicio
	}

	if err := tx.Create(&docEliminado).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al registrar en papelera"})
	}

	// Eliminar los índices de páginas relacionados con el documento activo
	if err := tx.Where("documento_id = ?", documento.ID).Delete(&models.IndicePagina{}).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar índices de páginas"})
	}

	// Eliminar el registro de documento activo (libera el índice único)
	if err := tx.Delete(&documento).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar registro de documento activo"})
	}

	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al confirmar los cambios"})
	}

	return c.JSON(fiber.Map{
		"message": "Documento movido a la papelera correctamente y registro activo liberado",
	})
}

// queryPapelera es un helper genérico para aplicar paginación y búsqueda en GORM para la papelera
func queryPapelera(c *fiber.Ctx, baseQuery *gorm.DB) error {
	pageStr := c.Query("page", "1")
	limitStr := c.Query("limit", "15")
	search := c.Query("search", "")
	fecha := c.Query("fecha", "")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 15
	}
	offset := (page - 1) * limit

	query := baseQuery

	// Filtro de búsqueda (Nombre de asociado o ID de archivo)
	if search != "" {
		searchQuery := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
		cleanIDQuery := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(search)), "#", "")

		if idVal, err := strconv.Atoi(cleanIDQuery); err == nil {
			query = query.Joins("LEFT JOIN asociados ON documentos_eliminados.asociado_id = asociados.id").
				Where("LOWER(asociados.nombre_completo) LIKE ? OR documentos_eliminados.id = ?", searchQuery, idVal)
		} else {
			query = query.Joins("LEFT JOIN asociados ON documentos_eliminados.asociado_id = asociados.id").
				Where("LOWER(asociados.nombre_completo) LIKE ?", searchQuery)
		}
	}

	// Filtro de fecha
	if fecha != "" {
		query = query.Where("DATE(documentos_eliminados.fecha_eliminacion) = ?", fecha)
	}

	var total int64
	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Model(&models.DocumentoEliminado{}).Count(&total).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al contar elementos de la papelera"})
	}

	var docs []models.DocumentoEliminado
	err = query.Select("documentos_eliminados.*").
		Preload("UsuarioElimino").
		Preload("UsuarioAsignado").
		Preload("Asociado").
		Order("documentos_eliminados.fecha_eliminacion desc").
		Limit(limit).
		Offset(offset).
		Find(&docs).Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener elementos de la papelera"})
	}

	totalPages := int(math.Ceil(float64(total) / float64(limit)))

	return c.JSON(fiber.Map{
		"data":  docs,
		"total": total,
		"page":  page,
		"limit": limit,
		"pages": totalPages,
	})
}

// ObtenerPapeleraGeneral obtiene todos los documentos eliminados en la papelera para administradores (PAGINADO)
func ObtenerPapeleraGeneral(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Acceso restringido a Administradores"})
	}

	baseQuery := db.DB.Model(&models.DocumentoEliminado{})
	return queryPapelera(c, baseQuery)
}

// ObtenerPapeleraUsuario obtiene los documentos de la papelera asignados al usuario actual (PAGINADO)
func ObtenerPapeleraUsuario(c *fiber.Ctx) error {
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "No autorizado"})
	}
	
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

	baseQuery := db.DB.Model(&models.DocumentoEliminado{}).Where("documentos_eliminados.usuario_asignado_id = ?", usuarioID)
	return queryPapelera(c, baseQuery)
}

// AsignarDocumentoPapelera asigna un documento de la papelera a un usuario específico
func AsignarDocumentoPapelera(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Acceso restringido a Administradores"})
	}

	idStr := c.Params("id")
	docID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento eliminado inválido"})
	}

	type AsignarReq struct {
		UsuarioAsignadoID uint `json:"usuario_asignado_id"`
	}
	var req AsignarReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Cuerpo de solicitud inválido"})
	}

	var doc models.DocumentoEliminado
	if err := db.DB.First(&doc, docID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado en papelera"})
	}

	now := time.Now()
	doc.UsuarioAsignadoID = &req.UsuarioAsignadoID
	doc.FechaAsignacion = &now

	if err := db.DB.Save(&doc).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al asignar documento"})
	}

	return c.JSON(fiber.Map{"message": "Documento asignado correctamente"})
}

// DescargarDocumentoPapelera genera un enlace firmado temporal para descargar el documento de la papelera
func DescargarDocumentoPapelera(c *fiber.Ctx) error {
	idStr := c.Params("id")
	docID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	var doc models.DocumentoEliminado
	if err := db.DB.First(&doc, docID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado en papelera"})
	}

	// Verificar pertenencia si no es admin
	isAdmin := isUserAdmin(c)
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	var usuarioID uint = 0
	if ok {
		if sub, ok := claims["sub"]; ok {
			if idFloat, ok := sub.(float64); ok {
				usuarioID = uint(idFloat)
			}
		}
	}

	if !isAdmin && (doc.UsuarioAsignadoID == nil || *doc.UsuarioAsignadoID != usuarioID) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tiene permiso para descargar este documento de la papelera"})
	}

	url, err := gcs.GenerarURLFirmada(doc.FilePathPapelera, 10*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al generar enlace de descarga"})
	}

	// Guardar estado de descarga
	doc.Descargado = true
	now := time.Now()
	doc.FechaDescarga = &now
	if err := db.DB.Save(&doc).Error; err != nil {
		fmt.Printf("[WARN] No se pudo guardar estado de descarga en papelera: %v\n", err)
	}

	return c.JSON(fiber.Map{"url": url})
}

// EliminarDocumentoPermanente borra permanentemente el archivo físico y lógico de la papelera
func EliminarDocumentoPermanente(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Acceso restringido a Administradores"})
	}

	idStr := c.Params("id")
	docID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	var doc models.DocumentoEliminado
	if err := db.DB.First(&doc, docID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado en papelera"})
	}

	// 1. Eliminar archivo físico en GCS
	if doc.FilePathPapelera != "" {
		go func(path string) {
			ctx := context.Background()
			_ = gcs.EliminarArchivo(ctx, path)
		}(doc.FilePathPapelera)
	}

	// 2. Eliminar registro lógico
	if err := db.DB.Delete(&doc).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al depurar registro de la papelera"})
	}

	return c.JSON(fiber.Map{"message": "Archivo físico y lógico depurado permanentemente de la papelera"})
}

// ObtenerUsuarios obtiene la lista de todos los usuarios registrados localmente (para asignación de papelera)
// GET /api/gestor/papelera/usuarios
func ObtenerUsuarios(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Acceso restringido a Administradores"})
	}

	var users []models.Usuario
	err := db.DB.Select("id, name").Order("name asc").Find(&users).Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener usuarios"})
	}
	return c.JSON(users)
}

// ObtenerPapeleraAsociado obtiene todos los documentos y hojas eliminadas en la papelera para un asociado específico
// GET /api/gestor/asociados/:id/papelera
func ObtenerPapeleraAsociado(c *fiber.Ctx) error {
	asociadoID := c.Params("id")

	var docs []models.DocumentoEliminado
	err := db.DB.Preload("UsuarioElimino").
		Preload("Asociado").
		Where("asociado_id = ?", asociadoID).
		Order("fecha_eliminacion desc").
		Find(&docs).Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener papelera del asociado"})
	}
	return c.JSON(docs)
}

// GenerarURLDocumentoPapelera genera una URL firmada de 1 minuto para visualizar un documento o página de la papelera
// GET /api/gestor/papelera/:id/url
func GenerarURLDocumentoPapelera(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	var doc models.DocumentoEliminado
	if err := db.DB.First(&doc, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado en la papelera"})
	}

	url, err := gcs.GenerarURLFirmada(doc.FilePathPapelera, 1*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al generar enlace temporal"})
	}

	return c.JSON(fiber.Map{"url": url})
}

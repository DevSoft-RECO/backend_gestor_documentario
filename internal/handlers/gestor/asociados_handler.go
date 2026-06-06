package gestor

import (
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
)

// BuscarAsociado busca por nombre, DPI o código de cliente
func BuscarAsociado(c *fiber.Ctx) error {
	query := c.Query("q")
	if query == "" {
		return c.JSON([]models.Asociado{})
	}

	var asociados []models.Asociado
	searchQuery := "%" + query + "%"
	
	err := db.DB.Where("nombre_completo ILIKE ? OR dpi = ? OR codigo_cliente = ?", searchQuery, query, query).
		Limit(10).
		Find(&asociados).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al buscar asociados"})
	}

	return c.JSON(asociados)
}

// CrearAsociado registra un nuevo asociado en el sistema
func CrearAsociado(c *fiber.Ctx) error {
	asociado := new(models.Asociado)
	if err := c.BodyParser(asociado); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	asociado.UsuarioID = getUsuarioIDFromClaims(c)

	// Validaciones de unicidad con mensajes de error descriptivos y personalizados

	// 1. Validar que el DPI no esté vacío y no esté duplicado
	if asociado.DPI == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El documento DPI es requerido"})
	}
	var countDPI int64
	if err := db.DB.Model(&models.Asociado{}).Where("dpi = ?", asociado.DPI).Count(&countDPI).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno al verificar el DPI"})
	}
	if countDPI > 0 {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "El documento DPI ya se encuentra registrado para otro asociado"})
	}

	// 2. Validar que el Código Cliente no esté duplicado (si se proporciona)
	if asociado.CodigoCliente != nil && *asociado.CodigoCliente != "" {
		var countCod int64
		if err := db.DB.Model(&models.Asociado{}).Where("codigo_cliente = ?", *asociado.CodigoCliente).Count(&countCod).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno al verificar el Código Cliente"})
		}
		if countCod > 0 {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "El Código Cliente ya se encuentra asignado a otro asociado"})
		}
	}

	if err := db.DB.Create(asociado).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al registrar asociado"})
	}

	return c.Status(fiber.StatusCreated).JSON(asociado)
}

// ObtenerAsociado devuelve los datos de un asociado específico por ID
func ObtenerAsociado(c *fiber.Ctx) error {
	id := c.Params("id")
	var asociado models.Asociado
	
	if err := db.DB.First(&asociado, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Asociado no encontrado"})
	}

	return c.JSON(asociado)
}

// UpdateAsociado modifica los datos de un asociado existente
func UpdateAsociado(c *fiber.Ctx) error {
	id := c.Params("id")
	var asociado models.Asociado
	if err := db.DB.First(&asociado, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Asociado no encontrado"})
	}

	usuarioID := getUsuarioIDFromClaims(c)
	isSuperAdmin := isUserSuperAdmin(c)

	// Validar: Solo puede editar el usuario que creó el registro o un Super Admin
	if asociado.UsuarioID != usuarioID && !isSuperAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos para editar este asociado. Solo el creador o un Super Admin pueden modificarlo."})
	}

	// Parsear datos para actualizar
	updateData := new(models.Asociado)
	if err := c.BodyParser(updateData); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	// Validar que el DPI no esté vacío y no esté duplicado por otro asociado
	if updateData.DPI == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El documento DPI es requerido"})
	}
	var countDPI int64
	if err := db.DB.Model(&models.Asociado{}).Where("dpi = ? AND id != ?", updateData.DPI, asociado.ID).Count(&countDPI).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno al verificar el DPI"})
	}
	if countDPI > 0 {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "El documento DPI ya se encuentra registrado para otro asociado"})
	}

	// Validar Código Cliente
	if updateData.CodigoCliente != nil && *updateData.CodigoCliente != "" {
		var countCod int64
		if err := db.DB.Model(&models.Asociado{}).Where("codigo_cliente = ? AND id != ?", *updateData.CodigoCliente, asociado.ID).Count(&countCod).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno al verificar el Código Cliente"})
		}
		if countCod > 0 {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "El Código Cliente ya se encuentra asignado a otro asociado"})
		}
	}

	// Actualizar campos permitidos
	asociado.NombreCompleto = updateData.NombreCompleto
	asociado.DPI = updateData.DPI
	asociado.CodigoCliente = updateData.CodigoCliente
	asociado.Direccion = updateData.Direccion

	if err := db.DB.Save(&asociado).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar asociado"})
	}

	return c.JSON(asociado)
}

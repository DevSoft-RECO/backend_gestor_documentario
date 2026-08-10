package middleware

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func RequirePermission(requiredPerm string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("userClaims").(jwt.MapClaims)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"detail": "No autorizado"})
		}

		sub := fmt.Sprintf("%v", claims["sub"])
		userID, err := strconv.Atoi(sub)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"detail": "ID de usuario inválido en token"})
		}

		var usuario models.Usuario
		if err := db.DB.First(&usuario, userID).Error; err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"detail": "Usuario no encontrado"})
		}

		var roles []string
		if usuario.Roles != nil {
			json.Unmarshal([]byte(*usuario.Roles), &roles)
		}

		isSuperAdmin := false
		for _, r := range roles {
			if r == "Super Admin" {
				isSuperAdmin = true
				break
			}
		}

		var perms []string
		if usuario.Permissions != nil {
			json.Unmarshal([]byte(*usuario.Permissions), &perms)
		}

		hasPerm := false
		for _, p := range perms {
			if p == requiredPerm {
				hasPerm = true
				break
			}
		}

		if !hasPerm && !isSuperAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"detail": fmt.Sprintf("Acceso denegado: se requiere el permiso '%s' o rol Super Admin", requiredPerm)})
		}

		return c.Next()
	}
}

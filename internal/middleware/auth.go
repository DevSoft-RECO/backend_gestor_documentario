package middleware

import (
	"strings"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/auth"
	"github.com/gofiber/fiber/v2"
)

func AuthRequired(c *fiber.Ctx) error {
	rawAuth := c.Get("Authorization")
	if rawAuth == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"detail": "No se proporcionó token"})
	}

	token := strings.Replace(rawAuth, "Bearer ", "", 1)
	
	claims, err := auth.VerifyToken(token)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"detail": "Token inválido o expirado", "error": err.Error()})
	}

	c.Locals("userClaims", claims)

	return c.Next()
}

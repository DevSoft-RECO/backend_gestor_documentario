package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/auth"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
)

func MeHandler(c *fiber.Ctx) error {
	rawAuth := c.Get("Authorization")
	if rawAuth == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"detail": "No se proporcionó token"})
	}

	token := strings.Replace(rawAuth, "Bearer ", "", 1)
	claims, err := auth.VerifyToken(token)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"detail": "Token inválido o expirado", "error": err.Error()})
	}

	sub := fmt.Sprintf("%v", claims["sub"])
	userID, _ := strconv.Atoi(sub)
	jti := fmt.Sprintf("%v", claims["jti"])
	force := c.Query("force") == "true"

	var usuario models.Usuario
	result := db.DB.First(&usuario, userID)

	// Local Cache Strategy
	if !force && result.Error == nil && usuario.JTI != nil && *usuario.JTI == jti {
		var agencia models.Agencia
		agenciaNombre := ""
		if usuario.IDAgencia != nil {
			if db.DB.First(&agencia, *usuario.IDAgencia).Error == nil {
				agenciaNombre = agencia.Nombre
			}
		}

		return c.JSON(fiber.Map{
			"logged_in":   true,
			"user_id":     sub,
			"email":       usuario.Email,
			"name":        usuario.Name,
			"username":    usuario.Username,
			"id_agencia":  usuario.IDAgencia,
			"agencia":     agenciaNombre,
			"avatar":      usuario.Avatar,
			"telefono":    usuario.Telefono,
			"roles":       parseJSONArray(usuario.Roles),
			"permissions": parseJSONArray(usuario.Permissions),
			"_source":     "cache",
		})
	}

	// Fetch from Mother App
	motherData, err := auth.FetchUserDataFromMother(rawAuth)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"logged_in": true,
			"user_id":   sub,
			"_warning":  "No se pudo contactar a la App Madre. Perfil parcial.",
			"error":     err.Error(),
		})
	}

	// Normalize roles and permissions
	roles := normalizeList(motherData.Roles)
	permissions := normalizeList(motherData.Permissions)
	if len(permissions) == 0 {
		permissions = normalizeList(motherData.Permisos)
	}

	// Upsert Agency
	if motherData.Agencia.ID != 0 {
		ag := models.Agencia{
			ID:        motherData.Agencia.ID,
			Nombre:    motherData.Agencia.Nombre,
			Codigo:    motherData.Agencia.Codigo,
			CodigoT24: motherData.Agencia.CodigoT24,
			Direccion: motherData.Agencia.Direccion,
		}
		db.DB.Save(&ag)
	}

	// Upsert User
	rolesJSON, _ := json.Marshal(roles)
	permsJSON, _ := json.Marshal(permissions)

	rolesStr := string(rolesJSON)
	permsStr := string(permsJSON)

	usuarioUpdate := models.Usuario{
		ID:          userID,
		Name:        &motherData.Name,
		Username:    &motherData.Username,
		Email:       &motherData.Email,
		Telefono:    &motherData.Telefono,
		IDAgencia:   motherData.IDAgencia,
		Avatar:      &motherData.Avatar,
		JTI:         &jti,
		Roles:       &rolesStr,
		Permissions: &permsStr,
	}
	db.DB.Save(&usuarioUpdate)

	return c.JSON(fiber.Map{
		"logged_in":   true,
		"user_id":     sub,
		"email":       motherData.Email,
		"name":        motherData.Name,
		"username":    motherData.Username,
		"id_agencia":  motherData.IDAgencia,
		"agencia":     motherData.Agencia.Nombre,
		"avatar":      motherData.Avatar,
		"telefono":    motherData.Telefono,
		"roles":       roles,
		"permissions": permissions,
		"_source":     "madre",
	})
}

func parseJSONArray(s *string) []string {
	if s == nil {
		return []string{}
	}
	var res []string
	json.Unmarshal([]byte(*s), &res)
	return res
}

func normalizeList(raw interface{}) []string {
	var list []string
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if name, ok := m["name"].(string); ok {
					list = append(list, name)
				}
			} else if s, ok := item.(string); ok {
				list = append(list, s)
			}
		}
	}
	return list
}

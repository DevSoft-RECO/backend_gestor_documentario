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
			"id_puesto":   usuario.IDPuesto,
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

	// Sincronización Pasiva del Puesto
	if motherData.Puesto.Nombre != "" {
		var puestoLocal models.Puesto
		if motherData.Puesto.ID > 0 {
			// Si tenemos el ID de la madre, forzamos ese ID localmente
			puestoLocal = models.Puesto{ID: motherData.Puesto.ID, Nombre: motherData.Puesto.Nombre}
			db.DB.Save(&puestoLocal)
		} else {
			// Si solo viene el nombre (string simple), buscamos o creamos
			db.DB.Where("nombre = ?", motherData.Puesto.Nombre).FirstOrCreate(&puestoLocal, models.Puesto{Nombre: motherData.Puesto.Nombre})
		}
		usuarioUpdate.IDPuesto = &puestoLocal.ID
	} else {
		usuarioUpdate.IDPuesto = nil
	}

	db.DB.Save(&usuarioUpdate)

	return c.JSON(fiber.Map{
		"logged_in":   true,
		"user_id":     sub,
		"email":       motherData.Email,
		"name":        motherData.Name,
		"username":    motherData.Username,
		"id_agencia":  motherData.IDAgencia,
		"id_puesto":   usuarioUpdate.IDPuesto,
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

func strPtrOrNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

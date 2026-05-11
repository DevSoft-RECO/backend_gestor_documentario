package auth

import (
	"crypto/rsa"
	"fmt"
	"os"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

var publicKey *rsa.PublicKey

func LoadPublicKey() error {
	keyBytes, err := os.ReadFile(config.Envs.OAuthPublicKeyPath)
	if err != nil {
		return fmt.Errorf("error leyendo llave pública: %v", err)
	}

	publicKey, err = jwt.ParseRSAPublicKeyFromPEM(keyBytes)
	if err != nil {
		return fmt.Errorf("error parseando llave pública RS256: %v", err)
	}

	return nil
}

func VerifyToken(tokenString string) (jwt.MapClaims, error) {
	if publicKey == nil {
		if err := LoadPublicKey(); err != nil {
			return nil, err
		}
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("método de firma inesperado: %v", token.Header["alg"])
		}
		return publicKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("token inválido")
}

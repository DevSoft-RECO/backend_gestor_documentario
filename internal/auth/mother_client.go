package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
)

type MotherUserData struct {
	ID          int         `json:"id"`
	Name        string      `json:"name"`
	Username    string      `json:"username"`
	Email       string      `json:"email"`
	Telefono    string      `json:"telefono"`
	Avatar      string      `json:"avatar"`
	IDAgencia   *int        `json:"idagencia"`
	Roles       interface{} `json:"roles"`
	Permissions interface{} `json:"permissions"`
	Permisos    interface{} `json:"permisos"` // Laravel might use permisos
	Agencia     struct {
		ID        int     `json:"id"`
		Nombre    string  `json:"nombre"`
		Codigo    *int    `json:"codigo"`
		CodigoT24 *string `json:"codigot24"`
		Direccion *string `json:"direccion"`
	} `json:"agencia"`
}

var client = &http.Client{
	Timeout: 15 * time.Second,
}

func FetchUserDataFromMother(authToken string) (*MotherUserData, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/user", config.Envs.MotherAPIURL), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", authToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("la App Madre respondió con status: %d", resp.StatusCode)
	}

	var data MotherUserData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

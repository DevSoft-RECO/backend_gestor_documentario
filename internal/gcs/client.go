package gcs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"cloud.google.com/go/storage"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"google.golang.org/api/option"
)

var (
	gcsClient *storage.Client
	bucketName string
	privateKey []byte
	clientEmail string
)

type credentials struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

// InitGCS inicializa el cliente de Google Cloud Storage
func InitGCS() error {
	if config.Envs.GCSKeyFile == "" || config.Envs.GCSBucketName == "" {
		return fmt.Errorf("GCS credentials or bucket name not set in config")
	}

	bucketName = config.Envs.GCSBucketName

	// Leer el archivo de credenciales para extraer el email y la llave privada para firmar URLs
	credBytes, err := os.ReadFile(config.Envs.GCSKeyFile)
	if err != nil {
		return fmt.Errorf("error leyendo archivo de llaves GCS: %w", err)
	}

	var creds credentials
	if err := json.Unmarshal(credBytes, &creds); err != nil {
		return fmt.Errorf("error al decodificar credenciales JSON: %w", err)
	}

	clientEmail = creds.ClientEmail
	privateKey = []byte(creds.PrivateKey)

	// Crear cliente de almacenamiento
	ctx := context.Background()
	client, err := storage.NewClient(ctx, option.WithCredentialsFile(config.Envs.GCSKeyFile))
	if err != nil {
		return fmt.Errorf("error creando cliente de almacenamiento GCS: %w", err)
	}

	gcsClient = client
	fmt.Printf("[GCS] Inicializado con éxito. Bucket: %s\n", bucketName)
	return nil
}

// SubirArchivo sube un flujo de datos (io.Reader) a GCS en la ruta de objeto dada
func SubirArchivo(ctx context.Context, objectName string, content io.Reader) error {
	if gcsClient == nil {
		if err := InitGCS(); err != nil {
			return err
		}
	}

	wc := gcsClient.Bucket(bucketName).Object(objectName).NewWriter(ctx)
	if _, err := io.Copy(wc, content); err != nil {
		wc.Close()
		return fmt.Errorf("error al escribir objeto en GCS: %w", err)
	}

	if err := wc.Close(); err != nil {
		return fmt.Errorf("error al cerrar escritor de GCS: %w", err)
	}

	return nil
}

// DescargarArchivoTemporal descarga un objeto de GCS a un archivo local temporal
// El llamador es responsable de borrar el archivo retornado mediante os.Remove()
func DescargarArchivoTemporal(ctx context.Context, objectName string) (string, error) {
	if gcsClient == nil {
		if err := InitGCS(); err != nil {
			return "", err
		}
	}

	rc, err := gcsClient.Bucket(bucketName).Object(objectName).NewReader(ctx)
	if err != nil {
		return "", fmt.Errorf("error al abrir lector de objeto GCS %s: %w", objectName, err)
	}
	defer rc.Close()

	// Crear archivo temporal
	tempFile, err := os.CreateTemp("", "gcs_temp_*.pdf")
	if err != nil {
		return "", fmt.Errorf("error al crear archivo temporal: %w", err)
	}
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, rc); err != nil {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("error al copiar contenido del objeto GCS a temporal: %w", err)
	}

	return tempFile.Name(), nil
}

// GenerarURLFirmada genera un enlace firmado con expiración corta (por defecto 1 minuto)
func GenerarURLFirmada(objectName string, expiration time.Duration) (string, error) {
	if gcsClient == nil {
		if err := InitGCS(); err != nil {
			return "", err
		}
	}

	if len(privateKey) == 0 || clientEmail == "" {
		return "", fmt.Errorf("credenciales incompletas para firmar URLs")
	}

	opts := &storage.SignedURLOptions{
		GoogleAccessID: clientEmail,
		PrivateKey:     privateKey,
		Method:         "GET",
		Expires:        time.Now().Add(expiration),
	}

	url, err := storage.SignedURL(bucketName, objectName, opts)
	if err != nil {
		return "", fmt.Errorf("error al firmar URL para %s: %w", objectName, err)
	}

	return url, nil
}

// EliminarArchivo remueve un archivo de GCS
func EliminarArchivo(ctx context.Context, objectName string) error {
	if gcsClient == nil {
		if err := InitGCS(); err != nil {
			return err
		}
	}

	err := gcsClient.Bucket(bucketName).Object(objectName).Delete(ctx)
	if err != nil && err != storage.ErrObjectNotExist {
		return fmt.Errorf("error al eliminar objeto de GCS: %w", err)
	}

	return nil
}

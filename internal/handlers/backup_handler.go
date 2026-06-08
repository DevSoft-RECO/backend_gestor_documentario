package handlers

// ==========================================
// === BACKUP SYSTEM ===
// Handler para el Sistema de Respaldos en Go (Gestor Documental)
// ==========================================

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/gofiber/fiber/v2"
)

// GenerateBackupHandler inicia el backup en background
// POST /api/internal/backup
func GenerateBackupHandler(c *fiber.Ctx) error {
	token := config.Envs.BackupMadreToken
	signature := c.Get("X-Signature")
	timestampStr := c.Get("X-Timestamp")

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Timestamp inválido"})
	}

	// 1. Validar expiración (máximo 5 minutos)
	if mathAbs(time.Now().Unix()-timestamp) > 300 {
		return c.Status(403).JSON(fiber.Map{"error": "Petición expirada"})
	}

	// Obtener el body crudo para validar la firma
	bodyBytes := c.Body()

	// 2. Validar firma HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(fmt.Sprintf("%d%s", timestamp, string(bodyBytes))))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSignature), []byte(signature)) {
		log.Println("Backup Go: Firma inválida recibida de la Madre.")
		return c.Status(401).JSON(fiber.Map{"error": "Firma no coincide o no autorizada"})
	}

	// 3. Parsear datos de la petición
	var requestData struct {
		CallbackURL string `json:"callback_url"`
		UserID      int    `json:"user_id"`
		AppKey      string `json:"app_key"`
	}

	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Payload inválido"})
	}

	// Crear carpeta backups si no existe
	backupDir := "./backups"
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "No se pudo crear carpeta de backups"})
	}

	// Limpieza de respaldos huérfanos anteriores (más de 1 hora de antigüedad)
	if files, err := os.ReadDir(backupDir); err == nil {
		for _, file := range files {
			if !file.IsDir() {
				fPath := filepath.Join(backupDir, file.Name())
				if info, err := os.Stat(fPath); err == nil {
					if time.Since(info.ModTime()).Hours() > 1.0 {
						_ = os.Remove(fPath)
					}
				}
			}
		}
	}

	ext := ".sql.gz"
	if runtime.GOOS == "windows" {
		ext = ".sql" // Windows local sin gzip
	}
	filename := fmt.Sprintf("backup_%s_%s%s", requestData.AppKey, time.Now().Format("20060102_150405"), ext)
	filePath := filepath.Join(backupDir, filename)

	// 4. Ejecutar el respaldo en background (Goroutine)
	go func() {
		log.Printf("Backup Go: Iniciando respaldo para %s en background...", filename)

		var cmd *exec.Cmd
		dbURL := config.Envs.DBURL

		pgDumpCmd := "pg_dump"
		if config.Envs.BackupPgDumpPath != "" {
			pgDumpCmd = config.Envs.BackupPgDumpPath
		}

		var stderr bytes.Buffer
		var runErr error

		if runtime.GOOS == "windows" {
			// En Windows (Desarrollo local con Laragon y PostgreSQL)
			// Abrimos el archivo físico y le redirigimos el stdout del ejecutable directamente sin pasar por cmd.exe
			outFile, err := os.Create(filePath)
			if err != nil {
				errStr := fmt.Sprintf("Error al crear archivo de backup en disco: %v", err)
				log.Printf("Backup Go: %s", errStr)
				sendCallbackToMother(requestData.CallbackURL, requestData.AppKey, filename, "failed", requestData.UserID, errStr, nil)
				return
			}
			
			cmd = exec.Command(pgDumpCmd, "--dbname=" + dbURL)
			cmd.Stdout = outFile
			cmd.Stderr = &stderr
			
			runErr = cmd.Run()
			outFile.Close() // Importante: cerrar el archivo para liberar el descriptor
		} else {
			// En Linux (Producción con PostgreSQL)
			// pg_dump URI | gzip > archivo.sql.gz (aquí sí usamos sh -c para canalizar con gzip)
			cmd = exec.Command("sh", "-c", fmt.Sprintf("\"%s\" \"%s\" | gzip > \"%s\"", pgDumpCmd, dbURL, filePath))
			cmd.Stderr = &stderr
			runErr = cmd.Run()
		}

		if runErr != nil {
			errStr := fmt.Sprintf("Error al ejecutar pg_dump: %v. Stderr: %s", runErr, stderr.String())
			log.Printf("Backup Go: %s", errStr)
			sendCallbackToMother(requestData.CallbackURL, requestData.AppKey, filename, "failed", requestData.UserID, errStr, nil)
			return
		}

		// Verificar que el archivo existe y pesa más de 0 bytes
		info, err := os.Stat(filePath)
		if err != nil || info.Size() == 0 {
			errStr := "Archivo de backup no fue creado o está vacío."
			log.Printf("Backup Go: %s", errStr)
			sendCallbackToMother(requestData.CallbackURL, requestData.AppKey, filename, "failed", requestData.UserID, errStr, nil)
			return
		}

		log.Printf("Backup Go: Respaldo completado exitosamente: %s", filename)
		fileSize := info.Size()
		sendCallbackToMother(requestData.CallbackURL, requestData.AppKey, filename, "success", requestData.UserID, "", &fileSize)
	}()

	return c.Status(202).JSON(fiber.Map{
		"status":  "success",
		"message": "Proceso de respaldo iniciado asíncronamente en la hija (Go).",
	})
}

// DownloadBackupHandler sirve el archivo
// GET /api/internal/download-backup
func DownloadBackupHandler(c *fiber.Ctx) error {
	token := config.Envs.BackupMadreToken
	filename := c.Query("file")
	timestampStr := c.Query("timestamp")
	signature := c.Query("signature")

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Timestamp inválido"})
	}

	// 1. Validar expiración (máximo 15 minutos)
	if mathAbs(time.Now().Unix()-timestamp) > 900 {
		return c.Status(403).JSON(fiber.Map{"error": "El enlace ha expirado"})
	}

	// 2. Validar firma HMAC-SHA256 (Formato manual idéntico al json_encode de PHP)
	payloadStr := fmt.Sprintf(`{"file":"%s","timestamp":%d}`, filename, timestamp)

	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(fmt.Sprintf("%d%s", timestamp, payloadStr)))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSignature), []byte(signature)) {
		log.Println("Backup Go: Intento de descarga con firma inválida.")
		return c.Status(401).JSON(fiber.Map{"error": "Firma de descarga inválida"})
	}

	filePath := filepath.Join("./backups", filename)

	// Verificar existencia del archivo
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("Backup Go: Archivo no encontrado: %s", filename)
		return c.Status(404).JSON(fiber.Map{"error": "El archivo ya no existe o ya fue descargado"})
	}

	log.Printf("Backup Go: Descargando archivo: %s", filename)

	// Servir archivo forzando descarga como adjunto (Attachment). El borrado lo controla la Madre.
	err = c.Download(filePath, filename)
	return err
}

// DeleteBackupHandler borra el archivo físico a petición de la Madre
// DELETE /api/internal/backup
func DeleteBackupHandler(c *fiber.Ctx) error {
	token := config.Envs.BackupMadreToken
	signature := c.Get("X-Signature")
	timestampStr := c.Get("X-Timestamp")

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Timestamp inválido"})
	}

	// 1. Validar expiración (máximo 5 minutos)
	if mathAbs(time.Now().Unix()-timestamp) > 300 {
		return c.Status(403).JSON(fiber.Map{"error": "Petición expirada"})
	}

	// Obtener el body crudo para validar la firma
	bodyBytes := c.Body()

	// 2. Validar firma HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(fmt.Sprintf("%d%s", timestamp, string(bodyBytes))))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSignature), []byte(signature)) {
		log.Println("Backup Go: Firma de borrado inválida recibida de la Madre.")
		return c.Status(401).JSON(fiber.Map{"error": "Firma no coincide o no autorizada"})
	}

	// 3. Parsear datos de la petición
	var requestData struct {
		File string `json:"file"`
	}

	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Payload inválido"})
	}

	filePath := filepath.Join("./backups", requestData.File)

	// Verificar existencia del archivo
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("Backup Go: Archivo no encontrado para borrar: %s", requestData.File)
		return c.Status(404).JSON(fiber.Map{"error": "El archivo no existe"})
	}

	if err := os.Remove(filePath); err != nil {
		log.Printf("Backup Go: Error al borrar archivo %s: %v", requestData.File, err)
		return c.Status(500).JSON(fiber.Map{"error": "No se pudo borrar el archivo"})
	}

	log.Printf("Backup Go: Archivo borrado exitosamente a petición de la Madre: %s", requestData.File)
	return c.JSON(fiber.Map{
		"status":  "success",
		"message": "Archivo eliminado correctamente.",
	})
}

// Helpers
func mathAbs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func sendCallbackToMother(callbackURL string, appKey string, filename string, status string, userID int, errStr string, size *int64) {
	token := config.Envs.BackupMadreToken
	timestamp := time.Now().Unix()

	type CallbackPayload struct {
		AppKey    string  `json:"app_key"`
		File      string  `json:"file"`
		Status    string  `json:"status"`
		UserID    int     `json:"user_id"`
		Timestamp int64   `json:"timestamp"`
		Error     *string `json:"error"`
		Size      *int64  `json:"size,omitempty"`
	}

	var errorVal *string
	if errStr != "" {
		errorVal = &errStr
	}

	payload := CallbackPayload{
		AppKey:    appKey,
		File:      filename,
		Status:    status,
		UserID:    userID,
		Timestamp: timestamp,
		Error:     errorVal,
		Size:      size,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Backup Go Callback: Error al codificar JSON: %v", err)
		return
	}

	bodyJSON := string(bodyBytes)

	// Firmar
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(fmt.Sprintf("%d%s", timestamp, bodyJSON)))
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest("POST", callbackURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Printf("Backup Go Callback: Error al crear request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", signature)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", timestamp))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Backup Go Callback: Error al enviar POST a la Madre: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("Backup Go Callback: Respuesta de la Madre recibida: %d", resp.StatusCode)
}

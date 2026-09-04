package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func isTextScanned(text string) bool {
	text = strings.TrimSpace(text)
	// Comprobar si contiene caracteres alfanuméricos legibles
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return false // PDF Digital (tiene texto nativo legible)
		}
	}
	return true // PDF Escaneado (solo imágenes)
}

// isScannedPDF detecta si un PDF es probablemente escaneado (si no contiene texto alfanumérico en su primera página)
func isScannedPDF(ctx context.Context, executable, filePath string) bool {
	// Timeout estricto de 10 segundos para evitar procesos colgados (zombies)
	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Intentar primero con pdftotext (poppler-utils) si está instalado (más rápido y seguro)
	if _, err := exec.LookPath("pdftotext"); err == nil {
		cmd := exec.CommandContext(ctxTimeout, "pdftotext", "-f", "1", "-l", "1", filePath, "-")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err == nil {
			return isTextScanned(stdout.String())
		}
	}

	// Fallback a Ghostscript escribiendo a un archivo temporal (evita bloqueos de buffer por stdout)
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("extract_%d.txt", time.Now().UnixNano()))
	defer os.Remove(tempFile)

	cmd := exec.CommandContext(ctxTimeout, executable,
		"-sDEVICE=txtwrite",
		"-dFirstPage=1",
		"-dLastPage=1",
		"-dNOPAUSE",
		"-dBATCH",
		"-dQUIET",
		"-sOutputFile="+tempFile, // No enviamos a "-" para prevenir cuelgues
		filePath,
	)

	if err := cmd.Run(); err != nil {
		return true // Por seguridad, asumimos escaneado si hay error
	}

	content, err := os.ReadFile(tempFile)
	if err != nil {
		return true
	}

	return isTextScanned(string(content))
}

// CompressPDF reduce el tamaño de un PDF usando Ghostscript
func CompressPDF(ctx context.Context, inputPath, outputPath string) error {
	// Añadir timeout de 60 segundos para evitar procesos zombie durante compresión
	ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Detectar el ejecutable según el sistema operativo
	executable := "gs"
	if runtime.GOOS == "windows" {
		executable = "gswin64c" // Nombre del ejecutable de consola en Windows
	}

	// Determinar el DPI según si el documento es digital o escaneado
	targetDPI := "100" // Para PDFs digitales (texto nativo)
	if isScannedPDF(ctxTimeout, executable, inputPath) {
		targetDPI = "170" // Mayor calidad/resolución para PDFs escaneados (imágenes legibles)
	}

	cmd := exec.CommandContext(ctxTimeout, executable,
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.4",
		"-dNOPAUSE",
		"-dQUIET",
		"-dBATCH",
		"-dDownsampleColorImages=true",
		"-dColorImageResolution="+targetDPI,
		"-dColorImageDownsampleThreshold=1.0",
		"-dDownsampleGrayImages=true",
		"-dGrayImageResolution="+targetDPI,
		"-dGrayImageDownsampleThreshold=1.0",
		"-dDownsampleMonoImages=true",
		"-dMonoImageResolution="+targetDPI,
		"-dMonoImageDownsampleThreshold=1.0",
		"-sOutputFile="+outputPath,
		inputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error al ejecutar ghostscript (%s): %w. Detalle: %s", executable, err, stderr.String())
	}

	return nil
}

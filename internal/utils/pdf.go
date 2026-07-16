package utils

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// isScannedPDF detecta si un PDF es probablemente escaneado (si no contiene texto alfanumérico en su primera página)
func isScannedPDF(ctx context.Context, executable, filePath string) bool {
	cmd := exec.CommandContext(ctx, executable,
		"-sDEVICE=txtwrite",
		"-dFirstPage=1",
		"-dLastPage=1",
		"-dNOPAUSE",
		"-dBATCH",
		"-dQUIET",
		"-sOutputFile=-",
		filePath,
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return true // Por seguridad, asumimos escaneado si hay error
	}

	text := strings.TrimSpace(stdout.String())
	// Comprobar si contiene caracteres alfanuméricos legibles
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return false // PDF Digital (tiene texto nativo legible)
		}
	}

	return true // PDF Escaneado (solo imágenes)
}

// CompressPDF reduce el tamaño de un PDF usando Ghostscript
func CompressPDF(ctx context.Context, inputPath, outputPath string) error {
	// Detectar el ejecutable según el sistema operativo
	executable := "gs"
	if runtime.GOOS == "windows" {
		executable = "gswin64c" // Nombre del ejecutable de consola en Windows
	}

	// Determinar el DPI según si el documento es digital o escaneado
	targetDPI := "100" // Para PDFs digitales (texto nativo)
	if isScannedPDF(ctx, executable, inputPath) {
		targetDPI = "170" // Mayor calidad/resolución para PDFs escaneados (imágenes legibles)
	}

	cmd := exec.CommandContext(ctx, executable,
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

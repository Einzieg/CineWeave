package provider

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
)

const (
	imageBlankBandMinimumFraction = 0.20
	imageBlankRowPixelPercent     = 98
	imageQualityMaxSampleColumns  = 512
	imageQualityMaxSampleRows     = 2048
)

func validateGatewayImageVisualQuality(media gatewayImageMedia) error {
	reader, closeReader, err := gatewayImageReader(media)
	if err != nil {
		return imageQualityError("供应商返回的图片无法读取，请重试")
	}
	if closeReader != nil {
		defer closeReader()
	}
	decoded, _, err := image.Decode(reader)
	if err != nil {
		return imageQualityError("供应商返回的图片无法解码，请重试")
	}
	if hasOversizedBlankEdgeBand(decoded) {
		return imageQualityError("供应商返回的图片包含过大的纯黑或透明空白区域，请重试")
	}
	return nil
}

func gatewayImageReader(media gatewayImageMedia) (io.Reader, func() error, error) {
	if len(media.Body) > 0 {
		return bytes.NewReader(media.Body), nil, nil
	}
	if strings.TrimSpace(media.TempPath) == "" {
		return nil, nil, fmt.Errorf("image media is empty")
	}
	file, err := os.Open(media.TempPath)
	if err != nil {
		return nil, nil, err
	}
	return file, file.Close, nil
}

func imageQualityError(message string) error {
	return &StandardErrorError{Standard: StandardError{
		Code:      CodeUpstreamOutputMismatch,
		Message:   message,
		Retryable: true,
	}}
}

func hasOversizedBlankEdgeBand(source image.Image) bool {
	bounds := source.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return true
	}
	xStep := sampleStep(width, imageQualityMaxSampleColumns)
	yStep := sampleStep(height, imageQualityMaxSampleRows)
	sampledRows := make([]bool, 0, (height+yStep-1)/yStep)
	for y := bounds.Min.Y; y < bounds.Max.Y; y += yStep {
		total := 0
		blank := 0
		for x := bounds.Min.X; x < bounds.Max.X; x += xStep {
			r, g, b, a := source.At(x, y).RGBA()
			total++
			if a <= 0x0808 || (r <= 0x0808 && g <= 0x0808 && b <= 0x0808) {
				blank++
			}
		}
		sampledRows = append(sampledRows, total > 0 && blank*100 >= total*imageBlankRowPixelPercent)
	}
	minimumBandRows := int(float64(len(sampledRows))*imageBlankBandMinimumFraction + 0.999999)
	if minimumBandRows < 2 {
		minimumBandRows = 2
	}
	return leadingTrueCount(sampledRows) >= minimumBandRows || trailingTrueCount(sampledRows) >= minimumBandRows
}

func sampleStep(size, maximumSamples int) int {
	if size <= maximumSamples {
		return 1
	}
	return (size + maximumSamples - 1) / maximumSamples
}

func leadingTrueCount(values []bool) int {
	count := 0
	for _, value := range values {
		if !value {
			break
		}
		count++
	}
	return count
}

func trailingTrueCount(values []bool) int {
	count := 0
	for index := len(values) - 1; index >= 0; index-- {
		if !values[index] {
			break
		}
		count++
	}
	return count
}

package utils

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
)

// ImageQuality 图片质量配置
type ImageQuality struct {
	MaxWidth    int  // 最大宽度
	MaxHeight   int  // 最大高度
	Quality     int  // 质量 (1-100)
	MaxSizeKB   int  // 最大文件大小 (KB)
	ConvertWebP bool // 是否转换为 WebP 格式
}

// DefaultImageQuality 默认图片质量配置
var DefaultImageQuality = ImageQuality{
	MaxWidth:    1920,
	MaxHeight:   1920,
	Quality:     85,
	MaxSizeKB:   500,
	ConvertWebP: false, // 暂时禁用 WebP，使用 JPEG 压缩
}

// CompressImage 压缩图片并转换为 WebP 格式（可选）
// 输入：原始文件路径
// 输出：压缩后的字节数组，文件扩展名，错误
func CompressImage(filePath string, quality ImageQuality) ([]byte, string, error) {
	// 打开图片文件
	file, err := os.Open(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	// 解码图片
	srcImg, format, err := image.Decode(file)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode image: %w", err)
	}

	// 获取原始尺寸
	bounds := srcImg.Bounds()
	origWidth := bounds.Dx()
	origHeight := bounds.Dy()

	// 计算缩放后的尺寸
	newWidth, newHeight := origWidth, origHeight

	if quality.MaxWidth > 0 && origWidth > quality.MaxWidth {
		newWidth = quality.MaxWidth
		newHeight = origHeight * quality.MaxWidth / origWidth
	}

	if quality.MaxHeight > 0 && newHeight > quality.MaxHeight {
		newHeight = quality.MaxHeight
		newWidth = newWidth * quality.MaxHeight / newHeight
	}

	// 如果需要缩放
	var finalImg image.Image = srcImg
	if newWidth != origWidth || newHeight != origHeight {
		finalImg = resizeImage(srcImg, newWidth, newHeight)
	}

	// 压缩并转换为字节数组
	var buf bytes.Buffer
	var outputFormat string

	if quality.ConvertWebP {
		// 转换为 WebP 格式（暂不支持）
		outputFormat = ".jpg"
	}

	// 保持原始格式或转换为 JPEG
	switch format {
	case "png":
		if err := png.Encode(&buf, finalImg); err != nil {
			return nil, "", fmt.Errorf("failed to encode png: %w", err)
		}
		outputFormat = ".png"
	default:
		// JPEG 或其他格式
		if err := jpeg.Encode(&buf, finalImg, &jpeg.Options{
			Quality: quality.Quality,
		}); err != nil {
			return nil, "", fmt.Errorf("failed to encode jpeg: %w", err)
		}
		outputFormat = ".jpg"
	}

	// 检查文件大小
	if quality.MaxSizeKB > 0 {
		fileSizeKB := buf.Len() / 1024
		if fileSizeKB > quality.MaxSizeKB {
			// 如果文件仍然太大，降低质量重新压缩
			return reduceQuality(buf.Bytes(), outputFormat, quality.MaxSizeKB)
		}
	}

	return buf.Bytes(), outputFormat, nil
}

// resizeImage 缩放图片
func resizeImage(img image.Image, newWidth, newHeight int) image.Image {
	// 简单的最近邻缩放算法
	// 可以使用更高级的算法如 Lanczos 来获得更好的质量
	bounds := img.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()

	// 创建新的图像
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// 计算缩放比例
	xScale := float64(srcWidth) / float64(newWidth)
	yScale := float64(srcHeight) / float64(newHeight)

	// 逐像素缩放
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			srcX := (float64(x)+0.5)*xScale - 0.5
			srcY := (float64(y)+0.5)*yScale - 0.5
			x0 := clamp(int(math.Floor(srcX)), 0, srcWidth-1)
			y0 := clamp(int(math.Floor(srcY)), 0, srcHeight-1)
			x1 := clamp(x0+1, 0, srcWidth-1)
			y1 := clamp(y0+1, 0, srcHeight-1)
			dx := math.Max(0, math.Min(1, srcX-float64(x0)))
			dy := math.Max(0, math.Min(1, srcY-float64(y0)))
			dst.SetRGBA64(x, y, bilinearColor(img, bounds.Min.X+x0, bounds.Min.Y+y0, bounds.Min.X+x1, bounds.Min.Y+y1, dx, dy))
		}
	}

	return dst
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func bilinearColor(img image.Image, x0, y0, x1, y1 int, dx, dy float64) color.RGBA64 {
	weights := [4]float64{(1 - dx) * (1 - dy), dx * (1 - dy), (1 - dx) * dy, dx * dy}
	points := [4]color.Color{img.At(x0, y0), img.At(x1, y0), img.At(x0, y1), img.At(x1, y1)}
	var red, green, blue, alpha float64
	for index, point := range points {
		r, g, b, a := point.RGBA()
		red += float64(r) * weights[index]
		green += float64(g) * weights[index]
		blue += float64(b) * weights[index]
		alpha += float64(a) * weights[index]
	}
	return color.RGBA64{R: uint16(red + 0.5), G: uint16(green + 0.5), B: uint16(blue + 0.5), A: uint16(alpha + 0.5)}
}

// reduceQuality 降低图片质量直到文件大小符合要求
func reduceQuality(data []byte, format string, targetSizeKB int) ([]byte, string, error) {
	quality := 85
	step := 5

	for quality >= 20 {
		// 解码图片
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode image: %w", err)
		}

		// 重新压缩
		var buf bytes.Buffer
		if format == ".png" {
			if err := png.Encode(&buf, img); err != nil {
				return nil, "", err
			}
		} else {
			if err := jpeg.Encode(&buf, img, &jpeg.Options{
				Quality: quality,
			}); err != nil {
				return nil, "", err
			}
		}

		// 检查大小
		if buf.Len()/1024 <= targetSizeKB {
			return buf.Bytes(), format, nil
		}

		quality -= step
	}

	// 如果最低质量还是太大，返回最后一次压缩的结果
	return data, format, nil
}

// GetImageFormat 获取图片格式
func GetImageFormat(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, format, err := image.Decode(file)
	return format, err
}

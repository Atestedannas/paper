package utils

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
)

// ImageQuality 图片质量配置
type ImageQuality struct {
	MaxWidth   int   // 最大宽度
	MaxHeight  int   // 最大高度
	Quality    int   // 质量 (1-100)
	MaxSizeKB  int   // 最大文件大小 (KB)
	ConvertWebP bool  // 是否转换为 WebP 格式
}

// DefaultImageQuality 默认图片质量配置
var DefaultImageQuality = ImageQuality{
	MaxWidth:   1920,
	MaxHeight:  1920,
	Quality:    85,
	MaxSizeKB:  500,
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
			srcX := int(float64(x) * xScale)
			srcY := int(float64(y) * yScale)
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}

	return dst
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

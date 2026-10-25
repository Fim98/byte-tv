package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// updatePublicKeyB64 通过 -ldflags "-X main.updatePublicKeyB64=..." 注入（ed25519 公钥，base64）。
var updatePublicKeyB64 = ""

func updatePublicKey() []byte {
	if updatePublicKeyB64 == "" {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(updatePublicKeyB64)
	if err != nil {
		return nil
	}
	return key
}

// ==========================================================================
// 应用图标 —— 陶土瓷感底板 + 拟物毛玻璃亚克力播放键
//
// 高清母图以 1024x1024 嵌入二进制：
//   - embeddedIconMac: macOS HIG 标准，824 squircle 居中，四周 100 留白，带原生柔和漫反射投影
//   - embeddedIconFull: 满幅 1024 squircle，供 Windows / Linux 任务栏与窗口图标使用
//
// 任意尺寸调用时自动采用高质量面积加权降采样，保证在 16~512 各级像素下保持边缘清晰锐利。
// ==========================================================================

//go:embed assets/icon_mac.png
var embeddedIconMac []byte

//go:embed assets/icon_full.png
var embeddedIconFull []byte

// appIconPNG 满幅应用图标：Windows 任务栏 / .ico / 窗口与对话框图标。
func appIconPNG(size int) []byte {
	return resizeIcon(embeddedIconFull, size)
}

// appIconMacOSPNG macOS 应用图标：squircle 缩到 824 居中，四周留白 100，带 macOS 原生柔和投影。
func appIconMacOSPNG(size int) []byte {
	return resizeIcon(embeddedIconMac, size)
}

func resizeIcon(srcData []byte, targetSize int) []byte {
	if targetSize <= 0 || targetSize == 1024 {
		return srcData
	}
	srcImg, err := png.Decode(bytes.NewReader(srcData))
	if err != nil {
		return srcData
	}
	srcB := srcImg.Bounds()
	srcW := srcB.Dx()
	srcH := srcB.Dy()
	if srcW == targetSize && srcH == targetSize {
		return srcData
	}

	dst := image.NewNRGBA(image.Rect(0, 0, targetSize, targetSize))
	scaleX := float64(srcW) / float64(targetSize)
	scaleY := float64(srcH) / float64(targetSize)

	for y := 0; y < targetSize; y++ {
		for x := 0; x < targetSize; x++ {
			x0 := float64(x) * scaleX
			x1 := float64(x+1) * scaleX
			y0 := float64(y) * scaleY
			y1 := float64(y+1) * scaleY

			var rSum, gSum, bSum, aSum, totalWeight float64

			ix0 := int(math.Floor(x0))
			ix1 := int(math.Ceil(x1))
			iy0 := int(math.Floor(y0))
			iy1 := int(math.Ceil(y1))

			for sy := iy0; sy < iy1; sy++ {
				if sy < 0 || sy >= srcH {
					continue
				}
				wy := math.Min(float64(sy+1), y1) - math.Max(float64(sy), y0)
				if wy <= 0 {
					continue
				}
				for sx := ix0; sx < ix1; sx++ {
					if sx < 0 || sx >= srcW {
						continue
					}
					wx := math.Min(float64(sx+1), x1) - math.Max(float64(sx), x0)
					if wx <= 0 {
						continue
					}
					weight := wx * wy
					r, g, b, a := srcImg.At(sx, sy).RGBA()
					rSum += float64(r>>8) * weight
					gSum += float64(g>>8) * weight
					bSum += float64(b>>8) * weight
					aSum += float64(a>>8) * weight
					totalWeight += weight
				}
			}

			if totalWeight > 0 {
				dst.SetNRGBA(x, y, color.NRGBA{
					R: uint8(rSum/totalWeight + 0.5),
					G: uint8(gSum/totalWeight + 0.5),
					B: uint8(bSum/totalWeight + 0.5),
					A: uint8(aSum/totalWeight + 0.5),
				})
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, dst)
	return buf.Bytes()
}

// icoFromPNG 把 256px PNG 包成单图 ICO（Windows Vista+ 支持 PNG 压缩的 ICO 条目）。
func icoFromPNG(pngData []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, []uint16{0, 1, 1})
	buf.Write([]byte{0, 0, 0, 0}) // 宽高 0 表示 256，调色板 0，保留 0
	_ = binary.Write(&buf, binary.LittleEndian, []uint16{1, 32})
	_ = binary.Write(&buf, binary.LittleEndian, []uint32{uint32(len(pngData)), 22})
	buf.Write(pngData)
	return buf.Bytes()
}

package main

import (
	"bytes"
	"encoding/base64"
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

// appIconPNG 程序化绘制应用图标：圆角方块 + 品牌渐变 + 播放三角，避免仓库里放二进制素材。
func appIconPNG(size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	radius := s * 0.22
	margin := s * 0.06
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			alpha := roundedRectCoverage(fx, fy, margin, margin, s-margin, s-margin, radius)
			if alpha <= 0 {
				continue
			}
			t := (fx + fy) / (2 * s)
			r := lerp(255, 168, t)
			g := lerp(64, 32, t)
			b := lerp(96, 220, t)
			if inTriangle(fx, fy, s*0.40, s*0.30, s*0.40, s*0.70, s*0.72, s*0.50) {
				r, g, b = 255, 255, 255
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(r), uint8(g), uint8(b), uint8(255 * alpha)})
		}
	}
	return encodePNG(img)
}

// trayTemplatePNG macOS 菜单栏模板图标（黑色 + 透明，系统自动适配深浅色）。
func trayTemplatePNG() []byte {
	const size = 44
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			a := roundedRectCoverage(fx, fy, 4, 8, 40, 36, 6)
			if a > 0 && !inTriangle(fx, fy, 17, 14, 17, 30, 30, 22) {
				img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, uint8(255 * a)})
			}
		}
	}
	return encodePNG(img)
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func roundedRectCoverage(x, y, x0, y0, x1, y1, r float64) float64 {
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	d := math.Hypot(x-cx, y-cy) - r
	return math.Max(0, math.Min(1, 0.5-d))
}

func inTriangle(px, py, ax, ay, bx, by, cx, cy float64) bool {
	sign := func(x1, y1, x2, y2, x3, y3 float64) float64 { return (x1-x3)*(y2-y3) - (x2-x3)*(y1-y3) }
	d1, d2, d3 := sign(px, py, ax, ay, bx, by), sign(px, py, bx, by, cx, cy), sign(px, py, cx, cy, ax, ay)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

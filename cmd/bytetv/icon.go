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

const (
	iconKeyTrim = 50.0
	keyAX, keyAY = 290.0, 390.0
	keyBX, keyBY = 290.0, 750.0
	keyCX, keyCY = 590.0, 570.0
)

type point struct{ x, y float64 }

var trayPlanes = buildHalfPlanes(keyPolygon(840.0/360.0, 162-keyAX*840.0/360.0, 92-keyAY*840.0/360.0))

// trayTemplatePNG macOS 菜单栏模板图标（黑色 + 透明，系统自动反色适配深浅色）。
// 菜单栏尺寸下底板会吃掉三角一半的呼吸，所以只保留前键剪影。
func trayTemplatePNG() []byte {
	const size = 44
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	pxPerUnit := float64(size) / 1024.0
	step := 1.0 / float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			ux := (float64(x) + 0.5) * step * 1024
			uy := (float64(y) + 0.5) * step * 1024
			a := convexCoverage(trayPlanes, ux, uy, pxPerUnit)
			if a <= 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, uint8(255*a + 0.5)})
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

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// roundedRectCoverage 圆角矩形的像素覆盖度。坐标与 r 都在设计单位，pxPerUnit 把
// 距离换算成像素，保证任何输出尺寸下都有 1px 宽的抗锯齿带。
func roundedRectCoverage(x, y, x0, y0, x1, y1, r, pxPerUnit float64) float64 {
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	d := math.Hypot(x-cx, y-cy) - r
	return clamp01(0.5 - d*pxPerUnit)
}

// keyPolygon 生成播放键轮廓：三角形三条边各从顶点处留出 trim 段，顶角用二次
// 贝塞尔收成圆角，再整体按 s 缩放、平移到 (tx,ty)。顶点顺序与设计稿路径一致。
func keyPolygon(s, tx, ty float64) []point {
	a := point{keyAX, keyAY}
	b := point{keyBX, keyBY}
	c := point{keyCX, keyCY}

	unit := func(p, q point) point {
		dx, dy := q.x-p.x, q.y-p.y
		l := math.Hypot(dx, dy)
		return point{dx / l, dy / l}
	}
	offset := func(p, u point, d float64) point { return point{p.x + u.x*d, p.y + u.y*d} }

	uAB := unit(a, b)
	uBC := unit(b, c)
	uCA := unit(c, a)

	a1 := offset(a, uAB, iconKeyTrim)  // A 沿 AB 方向
	a2 := offset(a, uCA, -iconKeyTrim) // A 沿 AC 方向
	b1 := offset(b, uBC, iconKeyTrim)
	b2 := offset(b, uAB, -iconKeyTrim)
	c1 := offset(c, uCA, iconKeyTrim)
	c2 := offset(c, uBC, -iconKeyTrim)

	pts := []point{a1, b2}
	pts = appendQuad(pts, b2, b, b1, 8)
	pts = append(pts, c1)
	pts = appendQuad(pts, c1, c, c2, 8)
	pts = append(pts, a2)
	pts = appendQuad(pts, a2, a, a1, 8)

	// 最后一段采样收在 a1 上，与首点重合；这里去掉，否则闭合边长度为 0，
	// 会生成一条退化的半平面（法线为零向量），把整个距离场拉平。
	if n := len(pts); n > 1 && math.Hypot(pts[n-1].x-pts[0].x, pts[n-1].y-pts[0].y) < 1e-6 {
		pts = pts[:n-1]
	}

	for i := range pts {
		pts[i].x = pts[i].x*s + tx
		pts[i].y = pts[i].y*s + ty
	}
	return pts
}

// appendQuad 把二次贝塞尔 p0→p2（控制点 pc）除起点外按 n 段采样追加到 pts。
func appendQuad(pts []point, p0, pc, p2 point, n int) []point {
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		mt := 1 - t
		pts = append(pts, point{
			mt*mt*p0.x + 2*t*mt*pc.x + t*t*p2.x,
			mt*mt*p0.y + 2*t*mt*pc.y + t*t*p2.y,
		})
	}
	return pts
}

// halfPlane 凸多边形的一条边，存起点与外法线。
type halfPlane struct{ vx, vy, nx, ny float64 }

func buildHalfPlanes(pts []point) []halfPlane {
	cx, cy := 0.0, 0.0
	for _, p := range pts {
		cx += p.x
		cy += p.y
	}
	n := float64(len(pts))
	cx, cy = cx/n, cy/n

	out := make([]halfPlane, 0, len(pts))
	for i := range pts {
		a, b := pts[i], pts[(i+1)%len(pts)]
		ex, ey := b.x-a.x, b.y-a.y
		l := math.Hypot(ex, ey)
		if l == 0 {
			continue // 退化边直接丢弃：留下零法线会把最大距离恒拉成 0
		}
		nx, ny := ey/l, -ex/l
		if nx*(cx-a.x)+ny*(cy-a.y) > 0 { // 让法线指向外侧
			nx, ny = -nx, -ny
		}
		out = append(out, halfPlane{a.x, a.y, nx, ny})
	}
	return out
}

// convexCoverage 点落在凸多边形内部的覆盖度。对凸集，各边外法线方向距离的最大值
// 就是带符号距离（内部为负），因此交集判断与抗锯齿一次算完。
func convexCoverage(planes []halfPlane, px, py, pxPerUnit float64) float64 {
	d := math.Inf(-1)
	for _, h := range planes {
		if v := h.nx*(px-h.vx) + h.ny*(py-h.vy); v > d {
			d = v
		}
	}
	if math.IsInf(d, -1) {
		return 0
	}
	return clamp01(0.5 - d*pxPerUnit)
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

package main

import (
	"bytes"
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
// 应用图标 —— 「连播双键」
//
// 全部几何量都定义在 1024 的设计坐标系里，绘制时按目标像素换算，因此任意尺寸都
// 保持同一套比例与 1px 抗锯齿带。仓库里依旧不放任何二进制素材。
//
// 造型参数（1024 基准）：
//   底板     满幅圆角方块，圆角 230（22.5%，对齐 macOS squircle 曲率）
//   前键     300 × 360 的播放三角，三个顶角切成 50 圆角，纯白
//   后键     与前键同形，向右上错位 (140,-140)，白 30%
//   内环     2 单位宽的白 14% 描边，缩小后自然消失
//
// 错位量是这套图标唯一的可调参数：小于 9% 后键会读成一层投影，大于 18% 会散成
// 两个互不相干的图形。
//
// 实现要点：播放键的圆角是「边线不动、顶角用二次曲线切掉 t 段」，等价形状是一个
// 凸多边形。不要用「各边内缩 r」去求距离场 —— 那会把锐角三角形整体缩小一半。
// ==========================================================================

const (
	iconClayR = 217.0 // #D97757
	iconClayG = 119.0
	iconClayB = 87.0

	iconCorner   = 230.0 // 底板圆角
	iconKeyTrim  = 50.0  // 播放键顶角切除量
	iconGhostDX  = 140.0 // 后键错位量
	iconGhostDY  = -140.0
	iconGhostA   = 0.30 // 后键不透明度
	iconRingA    = 0.14 // 内环不透明度
	iconRingW    = 2.0  // 内环宽度
	iconInsetMac = 824.0 / 1024.0
)

// 前键三角形（未切角）
const (
	keyAX, keyAY = 290.0, 390.0
	keyBX, keyBY = 290.0, 750.0
	keyCX, keyCY = 590.0, 570.0
)

type point struct{ x, y float64 }

// 缓存：主键多边形、后键多边形、模板三角多边形（各自的半平面表示）
var (
	keyPlanes   = buildHalfPlanes(keyPolygon(1, 0, 0))
	ghostPlanes = buildHalfPlanes(keyPolygon(1, iconGhostDX, iconGhostDY))
	// 菜单栏：单键等比放大到 700 × 840 居中，顶点 A 落在 (162,92)
	trayPlanes = buildHalfPlanes(keyPolygon(840.0/360.0, 162-keyAX*840.0/360.0, 92-keyAY*840.0/360.0))
)

// appIconPNG 满幅应用图标：Windows 任务栏 / .ico / 窗口与对话框图标。
func appIconPNG(size int) []byte { return iconPNG(size, false) }

// appIconMacOSPNG macOS 应用图标：squircle 缩到 824 居中，四周留白 100，
// 在 Dock 里和系统应用的视觉重量才对得上。
func appIconMacOSPNG(size int) []byte { return iconPNG(size, true) }

func iconPNG(size int, macInset bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := 1.0
	if macInset {
		scale = iconInsetMac
	}
	pxPerUnit := scale * float64(size) / 1024.0
	step := 1.0 / float64(size)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// 像素中心 → 1024 设计坐标（macOS 模式先反向放大，让画面整体内缩）
			ux := (((float64(x)+0.5)*step-0.5)/scale + 0.5) * 1024
			uy := (((float64(y)+0.5)*step-0.5)/scale + 0.5) * 1024

			base := roundedRectCoverage(ux, uy, 0, 0, 1024, 1024, iconCorner, pxPerUnit)
			if base <= 0 {
				continue
			}
			inner := roundedRectCoverage(ux, uy, iconRingW, iconRingW, 1024-iconRingW, 1024-iconRingW, iconCorner-1, pxPerUnit)
			ring := math.Max(0, base-inner)

			ghost := convexCoverage(ghostPlanes, ux, uy, pxPerUnit) * base
			front := convexCoverage(keyPlanes, ux, uy, pxPerUnit) * base

			r, g, b := iconClayR, iconClayG, iconClayB
			r, g, b = lerp(r, 255, iconGhostA*ghost), lerp(g, 255, iconGhostA*ghost), lerp(b, 255, iconGhostA*ghost)
			r, g, b = lerp(r, 255, front), lerp(g, 255, front), lerp(b, 255, front)
			r, g, b = lerp(r, 255, iconRingA*ring), lerp(g, 255, iconRingA*ring), lerp(b, 255, iconRingA*ring)

			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r + 0.5),
				G: uint8(g + 0.5),
				B: uint8(b + 0.5),
				A: uint8(255*base + 0.5),
			})
		}
	}
	return encodePNG(img)
}

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

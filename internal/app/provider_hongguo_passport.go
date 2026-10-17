package app

// Passport 完整签名套件：SM3 摘要 + Medusa + Helios + Gorgon(v2) + AES 混淆。
//
// 来源：hongguo-tv（github.com/aston314/hongguo-tv）core/cmd/hongguotv/passport_sign.go，
// 逻辑保持一致以便日后对照上游更新。
//
// 为什么需要它：红果的 `/novel/player/video_model/v1/`（取流）对签名要求比
// 目录/详情接口更高 —— 只带 X-Khronos/X-Gorgon(旧版)/X-SS-STUB/X-SS-Req-Ticket
// 会得到 HTTP 200 + 空响应体。实测补齐 X-Medusa/X-Helios 与新版 X-Gorgon 后即可取到流。
// 目录、详情等接口继续用轻量的 signHongguoRequest（见 provider_hongguo_sign.go）。
//
// 所有签名在本地完成，不发往任何外部签名服务。

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func mustProtocolHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func protocolRandom() (uint32, error) {
	var b [4]byte
	_, e := rand.Read(b[:])
	return binary.LittleEndian.Uint32(b[:]), e
}

func protocolLE(n uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, n); return b }

// passportSM3 是国密 SM3（GB/T 32905）标准实现。
func passportSM3(data []byte) [32]byte {
	n := uint64(len(data)) * 8
	b := append([]byte(nil), data...)
	b = append(b, 0x80)
	for len(b)%64 != 56 {
		b = append(b, 0)
	}
	b = binary.BigEndian.AppendUint64(b, n)
	v := [8]uint32{0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600, 0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e}
	p0 := func(x uint32) uint32 { return x ^ bits.RotateLeft32(x, 9) ^ bits.RotateLeft32(x, 17) }
	p1 := func(x uint32) uint32 { return x ^ bits.RotateLeft32(x, 15) ^ bits.RotateLeft32(x, 23) }
	for len(b) > 0 {
		var w [68]uint32
		for i := 0; i < 16; i++ {
			w[i] = binary.BigEndian.Uint32(b[i*4:])
		}
		for i := 16; i < 68; i++ {
			w[i] = p1(w[i-16]^w[i-9]^bits.RotateLeft32(w[i-3], 15)) ^ bits.RotateLeft32(w[i-13], 7) ^ w[i-6]
		}
		a, c, d, e, f, g, h, k := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]
		for j := 0; j < 64; j++ {
			t := uint32(0x79cc4519)
			ff := a ^ c ^ d
			gg := f ^ g ^ h
			if j >= 16 {
				t = 0x7a879d8a
				ff = (a & c) | (a & d) | (c & d)
				gg = (f & g) | (^f & h)
			}
			s1 := bits.RotateLeft32(bits.RotateLeft32(a, 12)+f+bits.RotateLeft32(t, j), 7)
			s2 := s1 ^ bits.RotateLeft32(a, 12)
			u1 := ff + e + s2 + (w[j] ^ w[j+4])
			u2 := gg + k + s1 + w[j]
			e = d
			d = bits.RotateLeft32(c, 9)
			c = a
			a = u1
			k = h
			h = bits.RotateLeft32(g, 19)
			g = f
			f = p0(u2)
		}
		for i, x := range [8]uint32{a, c, d, e, f, g, h, k} {
			v[i] ^= x
		}
		b = b[64:]
	}
	var out [32]byte
	for i, x := range v {
		binary.BigEndian.PutUint32(out[i*4:], x)
	}
	return out
}

func passportIV(data []byte, iv uint32) uint32 {
	for i, x := range data {
		if i&1 == 0 {
			iv = (iv >> 4) ^ iv ^ (iv << 6) ^ uint32(x)
		} else {
			iv = ^((iv >> 7) ^ iv ^ (uint32(x) | (iv << 12)))
		}
	}
	return iv
}

func passportBranch(query string, body []byte, ts uint32) (uint32, [32]byte, [16]byte) {
	q := passportSM3([]byte(query))
	var m [16]byte
	if len(body) > 0 {
		m = md5.Sum(body)
	}
	iv := passportIV(protocolLE(ts), passportIV(m[:], passportIV(q[:], 0x20230928)))
	return iv, q, m
}

// passportTuneQuery 通过只调整毫秒级 _rticket 来选出一个受支持的 F13 分支。
// 桌面版 TuneBranch 用同样的办法规避庞大的 branch-one 表。
func passportTuneQuery(q url.Values, body []byte, stamp int64) (string, uint32, [32]byte, [16]byte, error) {
	for i := int64(0); i < 256; i++ {
		q.Set("_rticket", strconv.FormatInt(stamp+i, 10))
		raw := q.Encode()
		iv, hash, m := passportBranch(raw, body, uint32(stamp/1000))
		if (iv&15)%3 == 2 {
			return raw, iv, hash, m, nil
		}
	}
	return "", 0, [32]byte{}, [16]byte{}, errors.New("unable to select signature branch")
}

func passportF13(iv, ts uint32, q [32]byte, m [16]byte) []byte {
	group := int((iv&15 - 2) / 3)
	seeds := [5]uint32{0x8980f29b, 0xeb549c7f, 0xb08726db, 0xd40cb5e6, 0xe8f559e4}
	count := seeds[group] + ts
	shift := int((count + 5) & 7)
	msg := append(append(append([]byte(nil), q[:]...), m[:]...), protocolLE(ts)...)
	for _, x := range mustProtocolHex("8496779dd4150bf8") {
		msg = append(msg, byte((uint16(x)*257)>>shift))
	}
	msg = append(msg, 0xa0, 1, 0, 0)
	start := [4]uint32{0x79e0f2fb, 0xc8b52570, 0xebc2f8cd, 0x7c104d93}
	end := [4]uint32{0x19be4866, 0xe85986b4, 0xe19b326e, 0x71d1d7d4}
	for i := range start {
		start[i] = bits.RotateLeft32(start[i], -int(count&31))
		end[i] = bits.RotateLeft32(end[i], -int((count+6)&31))
	}
	a, b, c, d := start[0], start[1], start[2], start[3]
	shifts := [4][4]int{{7, 12, 17, 22}, {5, 9, 14, 20}, {4, 11, 16, 23}, {6, 10, 15, 21}}
	for i := 0; i < 64; i++ {
		var f uint32
		switch i / 16 {
		case 0:
			f = (b & c) | (^b & d)
		case 1:
			f = (b & d) | (c &^ d)
		case 2:
			f = b ^ c ^ d
		case 3:
			f = c ^ (b | ^d)
		}
		word := binary.LittleEndian.Uint32(msg[int(passportOrders[group*64+i])*4:])
		x := b + bits.RotateLeft32(a+f+word+bits.RotateLeft32(passportMD5Constants[i], -int((count+1)&31)), shifts[i/16][i%4])
		a, b, c, d = d, x, b, c
	}
	out := make([]byte, 0, 20)
	for i, x := range [4]uint32{a, b, c, d} {
		out = append(out, protocolLE((start[i]+x)^end[i])...)
	}
	check := uint32(0x20220420)
	for i, x := range out[:12] {
		if i&1 == 0 {
			check = ((check >> 3) ^ check) ^ uint32(x) ^ (check << 7)
		} else {
			check = ((check >> 5) ^ check) ^ ^(uint32(x) | (check << 11))
		}
	}
	return append(out, protocolLE((check|4)^0x1000000)...)
}

func passportHelios(ts, nonce uint32) string {
	key := md5.Sum(append(protocolLE(nonce), []byte("8662")...))
	raw := []byte(hex.EncodeToString(key[:]))
	k := binary.LittleEndian.Uint64(raw)
	pending := []uint64{binary.LittleEndian.Uint64(raw[8:]), binary.LittleEndian.Uint64(raw[16:]), binary.LittleEndian.Uint64(raw[24:])}
	rounds := []uint64{k}
	for i := 0; i < 33; i++ {
		x := (bits.RotateLeft64(pending[0], -8) + k) ^ uint64(i)
		pending = append(pending[1:], x)
		k = x ^ bits.RotateLeft64(k, 3)
		rounds = append(rounds, k)
	}
	plain := []byte(fmt.Sprintf("%d-1588093228-8662", ts))
	pad := 16 - len(plain)%16
	plain = append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := protocolLE(nonce)
	for len(plain) > 0 {
		a, b := binary.LittleEndian.Uint64(plain), binary.LittleEndian.Uint64(plain[8:])
		for _, r := range rounds {
			b = r ^ (a + bits.RotateLeft64(b, -8))
			a = b ^ bits.RotateLeft64(a, 3)
		}
		out = binary.LittleEndian.AppendUint64(out, a)
		out = binary.LittleEndian.AppendUint64(out, b)
		plain = plain[16:]
	}
	return base64.StdEncoding.EncodeToString(out)
}

func passportGorgon(query string, body []byte, ts uint32, nonce uint16) string {
	q := md5.Sum([]byte(query))
	var m [16]byte
	if len(body) > 0 {
		m = md5.Sum(body)
	}
	data := make([]byte, 20)
	copy(data, q[:4])
	copy(data[4:], m[:4])
	binary.LittleEndian.PutUint32(data[12:], 67503104)
	binary.BigEndian.PutUint32(data[16:], ts)
	key := [8]byte{0x4a, 0x40, 0x16, byte(nonce >> 8), 0x47, 0x6c, 1, byte(nonce)}
	var s [256]byte
	for i := range s {
		s[i] = byte(i)
	}
	j := 0
	for i := range s {
		j = (j + int(s[i]) + int(key[i%8])) & 255
		s[i] = s[j]
	}
	j = 0
	for i := range data {
		j += int(s[i+1])
		y := s[j&255]
		s[i+1] = y
		data[i] ^= s[(int(y)*2)&255]
	}
	for i := range data {
		data[i] = bits.RotateLeft8(data[i], 4)
		next := data[0]
		if i < 19 {
			next = data[i+1]
		}
		data[i] = ^(bits.Reverse8(data[i]^next) ^ 20)
	}
	out := []byte{0x84, 4, byte(nonce), byte(nonce >> 8), 0x40, 1}
	return hex.EncodeToString(append(out, data...))
}

type protocolProto []byte

func (p *protocolProto) sint(field int, n int64) {
	*p = binary.AppendUvarint(*p, uint64(field<<3))
	*p = binary.AppendUvarint(*p, uint64(n<<1)^uint64(n>>63))
}

func (p *protocolProto) blob(field int, b []byte) {
	if len(b) == 0 {
		return
	}
	*p = binary.AppendUvarint(*p, uint64(field<<3|2))
	*p = binary.AppendUvarint(*p, uint64(len(b)))
	*p = append(*p, b...)
}

func (p *protocolProto) text(field int, s string) { p.blob(field, []byte(s)) }

func (p *protocolProto) float(field int, n float32) {
	*p = binary.AppendUvarint(*p, uint64(field<<3|5))
	*p = binary.LittleEndian.AppendUint32(*p, math.Float32bits(n))
}

func passportProto(query string, ts, nonce uint32, deviceID string, q [32]byte, m [16]byte, f13 []byte) []byte {
	var device, env, report, count, p protocolProto
	device.sint(1, 1)
	device.sint(2, 2)
	device.text(3, "8662")
	device.text(4, deviceID)
	device.text(5, "Ai6svO3PyrwDOUSmO6ZcResxu")
	device.text(6, "!noperm!")
	for _, i := range []int{7, 8} {
		device.sint(i, -888888)
	}
	device.sint(9, 3)
	device.sint(10, -888888)
	device.text(11, "!notset!")
	device.text(12, "Asia/Shanghai,8")
	device.text(13, "zh_CN")
	device.sint(14, 4)
	for i, x := range []float32{255.24993896484375, 35.58599090576172, 3.467449188232422, 3.467449188232422, 255.1754913330078, 42.17544174194336} {
		device.float(i+16, x)
	}
	device.text(22, "16")
	device.sint(23, 41)
	device.sint(24, 36)
	for i := 25; i <= 28; i++ {
		n := int64(1728388016635)
		if i == 28 {
			n += 2
		}
		device.sint(i, n)
	}
	device.sint(29, -1)
	for i, x := range []string{"25053RT47C", "Redmi", "25053RT47C", "25053RT47C", "Redmi", "Redmi", "Redmi"} {
		device.text(i+30, x)
	}
	device.sint(38, 31)
	report.sint(1, int64(ts))
	report.sint(2, -2)
	report.sint(4, 200)
	env.sint(1, 110)
	env.sint(2, 146331399)
	env.sint(3, 146331396)
	env.sint(5, 7)
	env.text(6, "v04.06.04.03-bugfix")
	env.sint(7, 11001)
	env.blob(12, device)
	env.blob(13, report)
	env.text(14, "7.3.5.32")
	count.sint(1, 111)
	count.sint(2, 10)
	count.sint(3, 694367)
	count.sint(5, 586952199)
	p.blob(1, mustProtocolHex("f7e85ffad7d7dc3bd62ac87057cf6118"))
	p.sint(2, 3)
	p.sint(3, int64(nonce))
	p.text(4, "8662")
	p.text(5, deviceID)
	p.text(6, "1588093228")
	p.text(7, "7.3.5.32")
	p.text(8, "v04.06.04-ml-android")
	p.sint(9, 67503104)
	p.blob(10, mustProtocolHex("4001000000000000"))
	p.sint(12, int64(ts))
	p.blob(13, f13)
	p.blob(14, q[:6])
	p.blob(15, count)
	p.text(16, "AXYQOS6n2m60x1fVZHIrH3iol")
	p.sint(17, int64(ts))
	hash := passportSM3(append(append([]byte(query), m[:]...), []byte("none")...))
	p.blob(19, hash[:])
	p.text(20, "none")
	p.sint(21, 312)
	p.blob(23, env)
	p.text(24, `{"cmr":16777216,"cmr2":16777216,"un_h":1879194040,"vpn":0,"kd":0,"fkd":3672518972,"pd":-1872573247,"dyn":"","do":0,"tk":true}`)
	return p
}

func passportXMXOR(data, key []byte) []byte {
	out := make([]byte, len(data))
	for i, x := range data {
		k := (i * 4) & 28
		a, b := key[k], key[k+1]
		v := ^(bits.RotateLeft8(x, 4) + a) ^ b
		v = bits.RotateLeft8(v, 3) + b
		out[len(data)-1-i] = ^(v ^ a)
	}
	flag := out[len(out)-1] ^ out[len(out)-2]
	old := out[0]
	out[0] += ^flag
	out[1] += out[0] ^ out[len(out)-1] ^ 254
	out[2] += (flag - old) ^ bits.RotateLeft8(out[1], 3) ^ 2
	for i := 0; i < len(out)-4; i++ {
		out[i+3] += ^(bits.RotateLeft8(out[i+2], 3) ^ out[i+1] ^ byte(i+3))
	}
	out[len(out)-1] ^= out[len(out)-2]
	var sum byte
	for _, x := range out[1:] {
		sum += x
	}
	out[0] = (out[0] ^ out[1]) + sum
	return out
}

func passportAESV3(data []byte, ts uint32) ([]byte, error) {
	if len(data) < 248 {
		return nil, errors.New("signature payload is too short")
	}
	i := int(ts & 3)
	box := passportSBox[i*256 : (i+1)*256]
	con := [4][4]int{{1, 0, 2, 3}, {2, 0, 3, 1}, {0, 1, 3, 2}, {1, 0, 2, 3}}[i]
	order := [4][16]int{{0, 9, 14, 11, 4, 13, 2, 7, 8, 1, 6, 15, 12, 5, 10, 3}, {0, 9, 14, 15, 4, 13, 2, 7, 8, 1, 6, 3, 12, 5, 10, 11}, {0, 9, 14, 7, 4, 13, 2, 11, 8, 1, 6, 3, 12, 5, 10, 15}, {0, 9, 14, 11, 4, 13, 2, 7, 8, 1, 6, 15, 12, 5, 10, 3}}[i]
	init := [4]uint32{0xca025ddc, 0x823dc546, 0xc9420583, 0xc298225f}[i]
	key := mustProtocolHex("f1593376766ea98d34f31b057a9d5be4")
	var mk [48]byte
	for j := 0; j < 16; j++ {
		mk[j] = key[j] ^ byte(init>>((j%4)*8))
	}
	r := 8
	for j := 4; j < 12; j++ {
		prev := [4]byte{mk[j*4-4], mk[j*4-3], mk[j*4-2], mk[j*4-1]}
		if j%4 == 0 {
			prev = [4]byte{byte(init>>uint(r&24)) ^ box[prev[1]], box[prev[2]], box[prev[3]], box[prev[0]]}
		}
		r += 2
		for k := 0; k < 4; k++ {
			mk[j*4+k] = prev[k] ^ mk[j*4+k-16]
		}
	}
	block := func(src []byte) []byte {
		b := append([]byte(nil), src...)
		add := func(base int) {
			for j := range b {
				b[j] ^= mk[base+(j/4)*4+con[j%4]]
			}
		}
		add(0)
		for round := 1; round <= 2; round++ {
			prev := append([]byte(nil), b...)
			for j := range b {
				index := order[j]
				b[j] = box[prev[con[index/4]*4+index%4]]
			}
			if round == 1 {
				prev = append([]byte(nil), b...)
				for j := range b {
					b[j] = prev[(j/4)*4+con[j%4]]
				}
				xt := func(x byte) byte {
					if x&128 != 0 {
						return x<<1 ^ 0x1b
					}
					return x << 1
				}
				for col := 0; col < 4; col++ {
					a, c, d, e := b[col], b[4+col], b[8+col], b[12+col]
					t := a ^ c ^ d ^ e
					b[col] ^= t ^ xt(a^c)
					b[4+col] ^= t ^ xt(c^d)
					b[8+col] ^= t ^ xt(d^e)
					b[12+col] ^= t ^ xt(e^a)
				}
			}
			add(round * 16)
		}
		for j := range b {
			b[j] ^= mk[16+j]
		}
		return b
	}
	plain := make([]byte, 32)
	for j := 0; j < 31; j++ {
		d := data[j*8:]
		plain[j] = (d[0]>>4)&2 | d[1]&64 | (d[2]>>2)&1 | (d[3]<<3)&128 | (d[4]>>1)&4 | (d[5]<<3)&16 | (d[6]<<5)&32 | (d[7]>>4)&8
	}
	plain[31] = 1
	previous := mustProtocolHex("1fe109a4125283f418de9e051a969e12")
	encrypted := []byte{}
	for j := 0; j < 32; j += 16 {
		for k := 0; k < 16; k++ {
			plain[j+k] ^= previous[k]
		}
		previous = block(plain[j : j+16])
		encrypted = append(encrypted, previous...)
	}
	out := append([]byte{encrypted[31]}, data...)
	for j := 0; j < 31; j++ {
		d := out[1+j*8:]
		k := encrypted[j]
		d[0] = (d[0] &^ 32) | (k << 4 & 32)
		d[1] = (d[1] &^ 64) | (k & 64)
		d[2] = (d[2] &^ 4) | (k << 2 & 4)
		d[3] = (d[3] &^ 16) | (k >> 3 & 16)
		d[4] = (d[4] &^ 8) | (k << 1 & 8)
		d[5] = (d[5] &^ 2) | (k >> 3 & 2)
		d[6] = (d[6] &^ 1) | (k >> 5 & 1)
		d[7] = (d[7] & 127) | (k << 4 & 128)
	}
	return out, nil
}

func passportMedusa(query string, ts, nonce, hashRand, xmRand uint32, deviceID string, iv uint32, q [32]byte, m [16]byte) (string, error) {
	f13 := passportF13(iv, ts, q, m)
	p := passportProto(query, ts, nonce, deviceID, q, m, f13)
	signKey := mustProtocolHex("8ebdfa3806ecc5cee79423e6029ed82540bc2218bb7eaef71cb691f7aa8aa2f5")
	key := passportSM3(append(append(append([]byte(nil), signKey...), protocolLE(hashRand)...), signKey...))
	d1 := (hashRand >> 16) & 255
	seed := protocolLE(^((d1 << 11) | (hashRand >> 24) ^ (d1 >> 5) ^ d1))
	p = append(mustProtocolHex("4001000000000000"), passportXMXOR(p, key[:])...)
	for a, b := 0, len(p)-1; a < b; a, b = a+1, b-1 {
		p[a], p[b] = p[b], p[a]
	}
	for i := range p {
		p[i] ^= seed[(^i)&3]
	}
	check := uint32(0x18000001) | (uint32(q[0]&63) << 14) | (uint32(f13[0]&63) << 8)
	body := append(append(append([]byte{0x35}, protocolLE(xmRand)...), protocolLE(check)...), p...)
	body = append(body, protocolLE(hashRand)[2:]...)
	enc, err := passportAESV3(body, ts)
	if err != nil {
		return "", err
	}
	header := mustProtocolHex("03000000f7e85ffad7d7dc3bd62ac87057cf6118")
	for j := 0; j < 20; j += 4 {
		binary.LittleEndian.PutUint32(header[j:], binary.LittleEndian.Uint32(header[j:])^ts)
	}
	header = append(header, protocolLE(hashRand)[:2]...)
	header = append(header, 0, 1)
	return base64.StdEncoding.EncodeToString(append(header, enc...)), nil
}

// hongguoPassportKey 是 context 开关：值为设备 ID 时，hongguoAppRequest 改用完整
// passport 签名（见 provider_hongguo_app.go 的 prepare 回调）。只给对签名要求高的
// 端点（如 video_model 取流）设置；目录、详情等继续用轻量签名。
type hongguoPassportKey struct{}

// passportAppDeviceID 返回当前 client 的设备 ID，供需要完整签名的调用方放入 context。
func (downloader *Downloader) passportAppDeviceID() string {
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.state.DeviceID
}

// signPassportRequest 产出一组完整的 passport 签名头，并回传（可能被微调过 _rticket 的）
// 最终 query 字符串 —— 调用方必须用它替换 URL 上的 RawQuery，否则签名对不上。
func signPassportRequest(q url.Values, body []byte, stamp int64, deviceID string) (string, http.Header, error) {
	raw, iv, hash, m, err := passportTuneQuery(q, body, stamp)
	if err != nil {
		return "", nil, err
	}
	var n [4]uint32
	for i := range n {
		n[i], err = protocolRandom()
		if err != nil {
			return "", nil, err
		}
	}
	ts := uint32(stamp / 1000)
	medusa, err := passportMedusa(raw, ts, n[0], n[1], n[2], deviceID, iv, hash, m)
	if err != nil {
		return "", nil, err
	}
	h := make(http.Header)
	h.Set("X-Khronos", strconv.FormatUint(uint64(ts), 10))
	h.Set("X-SS-Req-Ticket", q.Get("_rticket"))
	h.Set("X-Helios", passportHelios(ts, n[3]))
	h.Set("X-Medusa", medusa)
	h.Set("X-Gorgon", passportGorgon(raw, body, ts, uint16(n[0])))
	if len(body) > 0 {
		h.Set("X-SS-STUB", strings.ToUpper(hex.EncodeToString(m[:])))
	}
	return raw, h, nil
}
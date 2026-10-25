//go:build windows

package main

import (
	"errors"
	"image"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Decodifica e codifica video con Media Foundation (incluso in Windows 10/11): niente ffmpeg.

var (
	mfplat      = windows.NewLazySystemDLL("mfplat.dll")
	mfreadwrite = windows.NewLazySystemDLL("mfreadwrite.dll")

	pMFStartup                   = mfplat.NewProc("MFStartup")
	pMFCreateAttributes          = mfplat.NewProc("MFCreateAttributes")
	pMFCreateMediaType           = mfplat.NewProc("MFCreateMediaType")
	pMFCreateSample              = mfplat.NewProc("MFCreateSample")
	pMFCreateMemoryBuffer        = mfplat.NewProc("MFCreateMemoryBuffer")
	pMFCreateSourceReaderFromURL = mfreadwrite.NewProc("MFCreateSourceReaderFromURL")
	pMFCreateSinkWriterFromURL   = mfreadwrite.NewProc("MFCreateSinkWriterFromURL")

	mfOnce sync.Once
	mfErr  error
)

func guid(s string) windows.GUID {
	g, _ := windows.GUIDFromString(s)
	return g
}

var (
	gEnableVideoProcessing = guid("{fb394f3d-ccf1-42ee-bbb3-f9b845d5681d}")
	gMTMajorType           = guid("{48eba18e-f8c9-4687-bf11-0a74c9f96a8f}")
	gMTSubtype             = guid("{f7e34c9a-42e8-4714-b74b-cb29d72c35e5}")
	gMTFrameSize           = guid("{1652c33d-d6b2-4012-b834-72030849a37d}")
	gMTFrameRate           = guid("{c459a2e8-3d2c-4e44-b132-fee5156c7bb0}")
	gMTDefaultStride       = guid("{644b4e48-1e02-4516-b0eb-c01ca9d49ac6}")
	gMTAvgBitrate          = guid("{20332624-fb0d-4d9e-bd0d-cbf6786c102e}")
	gMTInterlaceMode       = guid("{e2724bb8-e676-4806-b4b2-a8d6efb44ccd}")
	gMTPixelAspect         = guid("{c6376a1e-8d0a-4027-be45-6d9a0ad39bb6}")
	gMediaTypeVideo        = guid("{73646976-0000-0010-8000-00AA00389B71}")
	gFormatRGB32           = guid("{00000016-0000-0010-8000-00AA00389B71}")
	gFormatH264            = guid("{34363248-0000-0010-8000-00AA00389B71}")
	gPDDuration            = guid("{6c990d33-bb8e-477a-8598-0d5d96fcd88a}")
)

const (
	firstVideoStream = 0xFFFFFFFC
	allStreams       = 0xFFFFFFFE
	mediaSource      = 0xFFFFFFFF
)

// vcall chiama il metodo n-esimo della vtable di un oggetto COM.
func vcall(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(**[64]uintptr)(obj)
	all := append([]uintptr{uintptr(obj)}, args...)
	r, _, _ := syscall.SyscallN(vtbl[idx], all...)
	return r
}

func release(obj unsafe.Pointer) {
	if obj != nil {
		vcall(obj, 2)
	}
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

// Indici dei metodi (IUnknown 0-2, IMFAttributes 3-32).
const (
	attrGetUINT32 = 7
	attrGetUINT64 = 8
	attrSetUINT32 = 21
	attrSetUINT64 = 22
	attrSetGUID   = 24

	readerSetStreamSelection  = 4
	readerGetCurrentMediaType = 6
	readerSetCurrentMediaType = 7
	readerReadSample          = 9
	readerGetPresentationAttr = 12
	sampleSetSampleTime       = 36
	sampleSetSampleDuration   = 38
	sampleConvertToContiguous = 41
	sampleAddBuffer           = 42
	bufferLock                = 3
	bufferUnlock              = 4
	bufferSetCurrentLength    = 6
	writerAddStream           = 3
	writerSetInputMediaType   = 4
	writerBeginWriting        = 5
	writerWriteSample         = 6
	writerFinalize            = 11
)

func mfInit() error {
	mfOnce.Do(func() {
		if err := mfplat.Load(); err != nil {
			mfErr = err
			return
		}
		if hr, _, _ := pMFStartup.Call(0x20070, 0); failed(hr) {
			mfErr = errors.New("Media Foundation non disponibile")
		}
	})
	return mfErr
}

// withCOM esegue f su un thread con COM inizializzato.
func withCOM(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("errore nella decodifica video")
		}
	}()
	if err := mfInit(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := pCoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
	if !failed(hr) {
		defer pCoUninitialize.Call()
	}
	return f()
}

type videoInfo struct {
	first   image.Image
	seconds uint32
	w, h    int
}

func setGUID(obj unsafe.Pointer, key, val *windows.GUID) uintptr {
	return vcall(obj, attrSetGUID, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(val)))
}

func setU64(obj unsafe.Pointer, key *windows.GUID, v uint64) uintptr {
	return vcall(obj, attrSetUINT64, uintptr(unsafe.Pointer(key)), uintptr(v))
}

func setU32(obj unsafe.Pointer, key *windows.GUID, v uint32) uintptr {
	return vcall(obj, attrSetUINT32, uintptr(unsafe.Pointer(key)), uintptr(v))
}

// openReader apre un video convertendo i fotogrammi in RGB32.
func openReader(path string) (reader unsafe.Pointer, w, h, stride int, dur time.Duration, err error) {
	var attr unsafe.Pointer
	if hr, _, _ := pMFCreateAttributes.Call(uintptr(unsafe.Pointer(&attr)), 1); failed(hr) {
		return nil, 0, 0, 0, 0, errors.New("MFCreateAttributes")
	}
	defer release(attr)
	setU32(attr, &gEnableVideoProcessing, 1)
	p, _ := syscall.UTF16PtrFromString(path)
	if hr, _, _ := pMFCreateSourceReaderFromURL.Call(uintptr(unsafe.Pointer(p)), uintptr(attr), uintptr(unsafe.Pointer(&reader))); failed(hr) {
		return nil, 0, 0, 0, 0, errors.New("impossibile aprire il video")
	}
	vcall(reader, readerSetStreamSelection, allStreams, 0)
	vcall(reader, readerSetStreamSelection, firstVideoStream, 1)

	var mt unsafe.Pointer
	pMFCreateMediaType.Call(uintptr(unsafe.Pointer(&mt)))
	setGUID(mt, &gMTMajorType, &gMediaTypeVideo)
	setGUID(mt, &gMTSubtype, &gFormatRGB32)
	hr := vcall(reader, readerSetCurrentMediaType, firstVideoStream, 0, uintptr(mt))
	release(mt)
	if failed(hr) {
		release(reader)
		return nil, 0, 0, 0, 0, errors.New("formato video non supportato")
	}
	var vf videoFormat
	if !vf.read(reader) {
		release(reader)
		return nil, 0, 0, 0, 0, errors.New("tipo video")
	}
	w, h, stride = vf.w, vf.h, vf.stride

	var pv struct {
		vt         uint16
		r1, r2, r3 uint16
		val        uint64
		pad        uint64
	}
	if !failed(vcall(reader, readerGetPresentationAttr, mediaSource, uintptr(unsafe.Pointer(&gPDDuration)), uintptr(unsafe.Pointer(&pv)))) {
		dur = time.Duration(pv.val) * 100
	}
	if w <= 0 || h <= 0 {
		release(reader)
		return nil, 0, 0, 0, 0, errors.New("dimensioni video")
	}
	return reader, w, h, stride, dur, nil
}

const attrGetBlob = 15

var gMTMinDisplayAperture = guid("{d7388766-18fe-48c6-a177-ee894867c8c4}")

// read rilegge il formato corrente: va fatto all'apertura E ogni volta che il decoder
// segnala un cambio di formato (succede spesso dopo il primo fotogramma).
func (vf *videoFormat) read(reader unsafe.Pointer) bool {
	var cur unsafe.Pointer
	if failed(vcall(reader, readerGetCurrentMediaType, firstVideoStream, uintptr(unsafe.Pointer(&cur)))) || cur == nil {
		return false
	}
	defer release(cur)
	var fs uint64
	vcall(cur, attrGetUINT64, uintptr(unsafe.Pointer(&gMTFrameSize)), uintptr(unsafe.Pointer(&fs)))
	vf.w, vf.h = int(fs>>32), int(fs&0xffffffff)
	vf.stride = vf.w * 4
	var st uint32
	if !failed(vcall(cur, attrGetUINT32, uintptr(unsafe.Pointer(&gMTDefaultStride)), uintptr(unsafe.Pointer(&st)))) {
		vf.stride = int(int32(st))
	}
	vf.cx, vf.cy, vf.offX, vf.offY = vf.w, vf.h, 0, 0
	// area visibile: MFVideoArea = OffsetX(fixed 16.16), OffsetY(fixed 16.16), cx, cy
	var area [16]byte
	var got uint32
	if !failed(vcall(cur, attrGetBlob, uintptr(unsafe.Pointer(&gMTMinDisplayAperture)),
		uintptr(unsafe.Pointer(&area[0])), uintptr(len(area)), uintptr(unsafe.Pointer(&got)))) && got == 16 {
		ox := int(int16(uint16(area[2]) | uint16(area[3])<<8))
		oy := int(int16(uint16(area[6]) | uint16(area[7])<<8))
		cx := int(int32(uint32(area[8]) | uint32(area[9])<<8 | uint32(area[10])<<16 | uint32(area[11])<<24))
		cy := int(int32(uint32(area[12]) | uint32(area[13])<<8 | uint32(area[14])<<16 | uint32(area[15])<<24))
		if cx > 0 && cy > 0 && ox >= 0 && oy >= 0 && ox+cx <= vf.w && oy+cy <= vf.h {
			vf.cx, vf.cy, vf.offX, vf.offY = cx, cy, ox, oy
		}
	}
	return vf.w > 0 && vf.h > 0
}

// readFrames legge i fotogrammi (al massimo ~15 al secondo, ridotti a maxPx).
func readFrames(reader unsafe.Pointer, w, h, stride, maxPx, maxFrames int) ([]image.Image, []time.Duration) {
	var vf videoFormat
	if !vf.read(reader) {
		vf = videoFormat{w: w, h: h, stride: stride, cx: w, cy: h}
	}
	var frames []image.Image
	var stamps []int64
	empty := 0
	for len(frames) < maxFrames {
		var actual, flags uint32
		var ts int64
		var sample unsafe.Pointer
		hr := vcall(reader, readerReadSample, firstVideoStream, 0,
			uintptr(unsafe.Pointer(&actual)), uintptr(unsafe.Pointer(&flags)), uintptr(unsafe.Pointer(&ts)), uintptr(unsafe.Pointer(&sample)))
		if failed(hr) || flags&0x2 != 0 { // errore o fine del video
			release(sample)
			break
		}
		if flags&0x20 != 0 { // MF_SOURCE_READERF_CURRENTMEDIATYPECHANGED: le misure sono cambiate
			vf.read(reader)
		}
		if sample == nil {
			if empty++; empty > 50 {
				break
			}
			continue
		}
		if len(stamps) > 0 && ts-stamps[len(stamps)-1] < 660000 { // < 66 ms: salto
			release(sample)
			continue
		}
		var buf unsafe.Pointer
		if failed(vcall(sample, sampleConvertToContiguous, uintptr(unsafe.Pointer(&buf)))) {
			release(sample)
			continue
		}
		var ptr unsafe.Pointer
		var maxLen, curLen uint32
		if !failed(vcall(buf, bufferLock, uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&curLen)))) {
			if p, rows, ok := vf.pitch(int(curLen)); ok {
				src := unsafe.Slice((*byte)(ptr), curLen)
				cw, ch := vf.cx, vf.cy
				img := image.NewRGBA(image.Rect(0, 0, cw, ch))
				for y := 0; y < ch; y++ {
					sy := vf.offY + y
					if vf.stride < 0 { // immagine capovolta
						sy = rows - 1 - sy
					}
					off := sy*p + vf.offX*4
					if sy < 0 || off+cw*4 > len(src) {
						break
					}
					row := src[off : off+cw*4]
					d := img.Pix[y*img.Stride : y*img.Stride+cw*4]
					for x := 0; x < cw*4; x += 4 {
						d[x], d[x+1], d[x+2], d[x+3] = row[x+2], row[x+1], row[x], 255
					}
				}
				var out image.Image = img
				if maxPx > 0 && (cw > maxPx || ch > maxPx) {
					out = fitThumb(img, maxPx)
				}
				frames = append(frames, out)
				stamps = append(stamps, ts)
			}
			vcall(buf, bufferUnlock)
		}
		release(buf)
		release(sample)
	}
	delays := make([]time.Duration, len(frames))
	for i := range frames {
		if i+1 < len(stamps) {
			delays[i] = time.Duration(stamps[i+1]-stamps[i]) * 100
		} else if i > 0 {
			delays[i] = delays[i-1]
		} else {
			delays[i] = 100 * time.Millisecond
		}
	}
	return frames, delays
}

func decodeVideoFrames(path string, maxPx, maxFrames int) (frames []image.Image, delays []time.Duration, err error) {
	err = withCOM(func() error {
		reader, w, h, stride, _, e := openReader(path)
		if e != nil {
			return e
		}
		defer release(reader)
		frames, delays = readFrames(reader, w, h, stride, maxPx, maxFrames)
		if len(frames) == 0 {
			return errors.New("nessun fotogramma")
		}
		return nil
	})
	return
}

func probeVideo(path string) (vi videoInfo) {
	withCOM(func() error {
		reader, w, h, stride, dur, e := openReader(path)
		if e != nil {
			return e
		}
		defer release(reader)
		vi.w, vi.h, vi.seconds = w, h, uint32((dur+time.Second/2)/time.Second)
		if fr, _ := readFrames(reader, w, h, stride, 0, 1); len(fr) > 0 {
			vi.first = fr[0]
			sz := fr[0].Bounds().Size() // dimensioni visibili, senza i bordi di riempimento
			vi.w, vi.h = sz.X, sz.Y
		}
		return nil
	})
	return
}

// gifToMP4 converte una GIF animata in MP4 H.264, come fa WhatsApp quando si invia una GIF.
func gifToMP4(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	g, err := decodeGIFFile(f)
	f.Close()
	if err != nil {
		return err
	}
	frames, delays := compositeGIF(g)
	if len(frames) == 0 {
		return errors.New("GIF vuota")
	}
	// dimensioni pari, lato lungo al massimo 480
	b := frames[0].Bounds()
	w, h := b.Dx(), b.Dy()
	if w > 480 || h > 480 {
		if w >= h {
			w, h = 480, h*480/w
		} else {
			w, h = w*480/h, 480
		}
	}
	w, h = max(2, w&^1), max(2, h&^1)
	// le GIF brevissime si ripetono fino ad almeno 2 secondi
	var total time.Duration
	for _, d := range delays {
		total += max(d, 20*time.Millisecond)
	}
	repeat := 1
	for total*time.Duration(repeat) < 2*time.Second && repeat < 20 {
		repeat++
	}

	return withCOM(func() error {
		os.Remove(out)
		p, _ := syscall.UTF16PtrFromString(out)
		var writer unsafe.Pointer
		if hr, _, _ := pMFCreateSinkWriterFromURL.Call(uintptr(unsafe.Pointer(p)), 0, 0, uintptr(unsafe.Pointer(&writer))); failed(hr) {
			return errors.New("impossibile creare il video")
		}
		defer release(writer)
		size := uint64(w)<<32 | uint64(h)
		rate := uint64(25)<<32 | 1
		par := uint64(1)<<32 | 1

		var outT, inT unsafe.Pointer
		pMFCreateMediaType.Call(uintptr(unsafe.Pointer(&outT)))
		defer release(outT)
		setGUID(outT, &gMTMajorType, &gMediaTypeVideo)
		setGUID(outT, &gMTSubtype, &gFormatH264)
		setU32(outT, &gMTAvgBitrate, 900_000)
		setU32(outT, &gMTInterlaceMode, 2) // progressivo
		setU64(outT, &gMTFrameSize, size)
		setU64(outT, &gMTFrameRate, rate)
		setU64(outT, &gMTPixelAspect, par)
		var stream uint32
		if failed(vcall(writer, writerAddStream, uintptr(outT), uintptr(unsafe.Pointer(&stream)))) {
			return errors.New("codificatore H.264 non disponibile")
		}
		pMFCreateMediaType.Call(uintptr(unsafe.Pointer(&inT)))
		defer release(inT)
		setGUID(inT, &gMTMajorType, &gMediaTypeVideo)
		setGUID(inT, &gMTSubtype, &gFormatRGB32)
		setU32(inT, &gMTInterlaceMode, 2)
		setU64(inT, &gMTFrameSize, size)
		setU64(inT, &gMTFrameRate, rate)
		setU64(inT, &gMTPixelAspect, par)
		setU32(inT, &gMTDefaultStride, uint32(w*4))
		if failed(vcall(writer, writerSetInputMediaType, uintptr(stream), uintptr(inT), 0)) {
			return errors.New("formato di ingresso non accettato")
		}
		if failed(vcall(writer, writerBeginWriting)) {
			return errors.New("avvio codifica")
		}
		n := uint32(w * h * 4)
		var t int64
		for r := 0; r < repeat; r++ {
			for i, fr := range frames {
				img := fitExact(fr, w, h)
				var buf unsafe.Pointer
				if failed(callProc(pMFCreateMemoryBuffer, uintptr(n), uintptr(unsafe.Pointer(&buf)))) {
					return errors.New("memoria")
				}
				var ptr unsafe.Pointer
				var mx, cl uint32
				vcall(buf, bufferLock, uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&mx)), uintptr(unsafe.Pointer(&cl)))
				dst := unsafe.Slice((*byte)(ptr), n)
				for p := 0; p < len(img.Pix); p += 4 {
					dst[p], dst[p+1], dst[p+2], dst[p+3] = img.Pix[p+2], img.Pix[p+1], img.Pix[p], 255
				}
				vcall(buf, bufferUnlock)
				vcall(buf, bufferSetCurrentLength, uintptr(n))
				var sample unsafe.Pointer
				pMFCreateSample.Call(uintptr(unsafe.Pointer(&sample)))
				vcall(sample, sampleAddBuffer, uintptr(buf))
				d := max(delays[i], 20*time.Millisecond)
				dur := int64(d / 100)
				vcall(sample, sampleSetSampleTime, uintptr(t))
				vcall(sample, sampleSetSampleDuration, uintptr(dur))
				hr := vcall(writer, writerWriteSample, uintptr(stream), uintptr(sample))
				release(sample)
				release(buf)
				if failed(hr) {
					return errors.New("scrittura video")
				}
				t += dur
			}
		}
		if failed(vcall(writer, writerFinalize)) {
			return errors.New("chiusura video")
		}
		return nil
	})
}

func callProc(p *windows.LazyProc, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

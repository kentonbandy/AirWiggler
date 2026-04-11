package scanner

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	flaclib "github.com/mewkiz/flac"
	"github.com/tcolgate/mp3"
)

func flacDuration(path string) float64 {
	stream, err := flaclib.Open(path)
	if err != nil {
		return 0
	}
	defer stream.Close()
	info := stream.Info
	if info.SampleRate == 0 {
		return 0
	}
	return float64(info.NSamples) / float64(info.SampleRate)
}

func mp3Duration(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	d := mp3.NewDecoder(f)
	var frame mp3.Frame
	skipped := 0
	var total time.Duration
	for {
		if err := d.Decode(&frame, &skipped); err != nil {
			break
		}
		total += frame.Duration()
	}
	return total.Seconds()
}

// wavDuration reads the RIFF/WAVE header and returns duration in seconds.
// It scans chunks until it finds "fmt " (for byteRate) and "data" (for size).
func wavDuration(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return 0
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0
	}

	var byteRate uint32
	for {
		var header [8]byte
		if _, err := io.ReadFull(f, header[:]); err != nil {
			break
		}
		chunkID := string(header[0:4])
		chunkSize := binary.LittleEndian.Uint32(header[4:8])

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return 0
			}
			var fmt [16]byte
			if _, err := io.ReadFull(f, fmt[:]); err != nil {
				return 0
			}
			byteRate = binary.LittleEndian.Uint32(fmt[8:12])
			if chunkSize > 16 {
				if _, err := f.Seek(int64(chunkSize-16), io.SeekCurrent); err != nil {
					return 0
				}
			}
		case "data":
			if byteRate == 0 {
				return 0
			}
			return float64(chunkSize) / float64(byteRate)
		default:
			skip := int64(chunkSize)
			if chunkSize%2 != 0 {
				skip++
			}
			if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
				return 0
			}
		}
	}
	return 0
}

// m4aDuration returns duration from an M4A/AAC file by scanning ISO BMFF boxes
// for the mvhd box, which holds duration and timescale.
func m4aDuration(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var scanBoxes func(r io.Reader, limit int64) float64
	scanBoxes = func(r io.Reader, limit int64) float64 {
		var consumed int64
		for consumed < limit {
			var hdr [8]byte
			if _, err := io.ReadFull(r, hdr[:]); err != nil {
				return 0
			}
			consumed += 8
			boxSize := int64(binary.BigEndian.Uint32(hdr[0:4]))
			boxType := string(hdr[4:8])
			if boxSize < 8 {
				return 0
			}
			bodySize := boxSize - 8

			switch boxType {
			case "moov", "trak", "mdia":
				if v := scanBoxes(io.LimitReader(r, bodySize), bodySize); v > 0 {
					return v
				}
			case "mvhd":
				body := make([]byte, bodySize)
				if _, err := io.ReadFull(r, body); err != nil {
					return 0
				}
				version := body[0]
				if version == 1 && len(body) >= 32 {
					timescale := binary.BigEndian.Uint32(body[20:24])
					dur := binary.BigEndian.Uint64(body[24:32])
					if timescale == 0 {
						return 0
					}
					return float64(dur) / float64(timescale)
				} else if version == 0 && len(body) >= 20 {
					timescale := binary.BigEndian.Uint32(body[12:16])
					dur := binary.BigEndian.Uint32(body[16:20])
					if timescale == 0 {
						return 0
					}
					return float64(dur) / float64(timescale)
				}
				return 0
			default:
				if _, err := io.CopyN(io.Discard, r, bodySize); err != nil {
					return 0
				}
			}
			consumed += bodySize
		}
		return 0
	}

	fi, err := f.Stat()
	if err != nil {
		return 0
	}
	return scanBoxes(f, fi.Size())
}

// oggDuration returns duration from an Ogg file (Vorbis or Opus) by reading
// the identification header for the sample rate, then seeking to the last Ogg
// page to get the final granule position.
func oggDuration(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	firstPage, err := readOggPage(f)
	if err != nil || len(firstPage) == 0 {
		return 0
	}

	var sampleRate uint32
	var preSkip uint16
	codec := ""
	if len(firstPage) >= 7 && string(firstPage[1:7]) == "vorbis" {
		codec = "vorbis"
		if len(firstPage) >= 16 {
			sampleRate = binary.LittleEndian.Uint32(firstPage[12:16])
		}
	} else if len(firstPage) >= 8 && string(firstPage[0:8]) == "OpusHead" {
		codec = "opus"
		sampleRate = 48000
		if len(firstPage) >= 12 {
			preSkip = binary.LittleEndian.Uint16(firstPage[10:12])
		}
	}
	if sampleRate == 0 {
		return 0
	}

	fi, err := f.Stat()
	if err != nil {
		return 0
	}
	seekPos := fi.Size() - 65536
	if seekPos < 0 {
		seekPos = 0
	}
	if _, err := f.Seek(seekPos, io.SeekStart); err != nil {
		return 0
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		return 0
	}

	lastIdx := -1
	for i := len(tail) - 4; i >= 0; i-- {
		if tail[i] == 'O' && tail[i+1] == 'g' && tail[i+2] == 'g' && tail[i+3] == 'S' {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 || lastIdx+14 > len(tail) {
		return 0
	}

	granule := int64(binary.LittleEndian.Uint64(tail[lastIdx+6 : lastIdx+14]))
	if granule <= 0 {
		return 0
	}

	samples := float64(granule)
	if codec == "opus" {
		samples -= float64(preSkip)
	}
	return samples / float64(sampleRate)
}

// aiffDuration returns duration from an AIFF or AIFF-C file by reading the
// COMM chunk (numSampleFrames and sampleRate as 80-bit extended float).
func aiffDuration(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return 0
	}
	if string(hdr[0:4]) != "FORM" {
		return 0
	}
	formType := string(hdr[8:12])
	if formType != "AIFF" && formType != "AIFC" {
		return 0
	}

	for {
		var chunkHdr [8]byte
		if _, err := io.ReadFull(f, chunkHdr[:]); err != nil {
			break
		}
		chunkID := string(chunkHdr[0:4])
		chunkSize := int64(binary.BigEndian.Uint32(chunkHdr[4:8]))

		if chunkID == "COMM" {
			if chunkSize < 18 {
				return 0
			}
			comm := make([]byte, 18)
			if _, err := io.ReadFull(f, comm); err != nil {
				return 0
			}
			numFrames := binary.BigEndian.Uint32(comm[2:6])
			// 80-bit IEEE 754 extended precision: exponent(2) + mantissa(8)
			exp := int(binary.BigEndian.Uint16(comm[8:10])) & 0x7FFF
			mant := binary.BigEndian.Uint64(comm[10:18])
			sampleRate := float64(mant) * math.Ldexp(1, exp-16383-63)
			if sampleRate == 0 {
				return 0
			}
			return float64(numFrames) / sampleRate
		}

		skip := chunkSize
		if chunkSize%2 != 0 {
			skip++
		}
		if _, err := io.CopyN(io.Discard, f, skip); err != nil {
			break
		}
	}
	return 0
}

// readOggPage reads the segment data from the first Ogg page in r.
func readOggPage(r io.Reader) ([]byte, error) {
	var fixed [27]byte
	if _, err := io.ReadFull(r, fixed[:]); err != nil {
		return nil, err
	}
	if string(fixed[0:4]) != "OggS" {
		return nil, io.ErrUnexpectedEOF
	}
	numSegs := int(fixed[26])
	segTable := make([]byte, numSegs)
	if _, err := io.ReadFull(r, segTable); err != nil {
		return nil, err
	}
	var pageSize int
	for _, s := range segTable {
		pageSize += int(s)
	}
	data := make([]byte, pageSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

// durationFor dispatches to the correct duration parser by file extension.
func durationFor(path string) float64 {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return flacDuration(path)
	case ".mp3":
		return mp3Duration(path)
	case ".wav":
		return wavDuration(path)
	case ".m4a", ".aac":
		return m4aDuration(path)
	case ".ogg", ".opus":
		return oggDuration(path)
	case ".aif", ".aiff":
		return aiffDuration(path)
	}
	return 0
}

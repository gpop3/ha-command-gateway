package voice

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Recorder stocke le flux audio brut en mémoire de façon thread-safe
type Recorder struct {
	mu         sync.Mutex
	rawData    []byte
	maxSamples int
}

// NewRecorder crée un recorder avec une limite de buffer (en octets)
func NewRecorder(maxSamples int) *Recorder {
	return &Recorder{maxSamples: maxSamples}
}

// Write ajoute des données au buffer
func (r *Recorder) Write(data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rawData = append(r.rawData, data...)
	// Troncature si dépassement
	if len(r.rawData) > r.maxSamples {
		r.rawData = r.rawData[len(r.rawData)-r.maxSamples:]
	}
}

// GetRawBytes retourne une copie thread-safe du buffer brut
func (r *Recorder) GetRawBytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	dst := make([]byte, len(r.rawData))
	copy(dst, r.rawData)
	return dst
}

// GetWavBytes construit et retourne un buffer WAV 16kHz mono 16bit
func (r *Recorder) GetWavBytes() *bytes.Buffer {
	r.mu.Lock()
	defer r.mu.Unlock()

	buf := new(bytes.Buffer)
	n := len(r.rawData)

	buf.Write([]byte("RIFF"))
	writeUint32(buf, uint32(36+n))
	buf.Write([]byte("WAVEfmt "))
	writeUint32(buf, 16)    // Subchunk1Size
	writeUint16(buf, 1)     // PCM
	writeUint16(buf, 1)     // Mono
	writeUint32(buf, 16000) // SampleRate
	writeUint32(buf, 32000) // ByteRate
	writeUint16(buf, 2)     // BlockAlign
	writeUint16(buf, 16)    // BitsPerSample
	buf.Write([]byte("data"))
	writeUint32(buf, uint32(n))
	buf.Write(r.rawData)

	return buf
}

// Clear vide intégralement le buffer
func (r *Recorder) Clear() {
	r.mu.Lock()
	r.rawData = r.rawData[:0]
	r.mu.Unlock()
}

// ClearNotAll conserve les derniers `taille` octets (pré-roll)
func (r *Recorder) ClearNotAll(taille int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rawData) > taille {
		r.rawData = append([]byte(nil), r.rawData[len(r.rawData)-taille:]...)
	} else {
		r.rawData = r.rawData[:0]
	}
}

// EstSilence retourne true si le RMS du segment est inférieur au seuil
func EstSilence(audioData []byte) bool {
	n := len(audioData) / 2
	if n == 0 {
		return true
	}

	var sum float64
	for i := 0; i < n; i++ {
		val := int16(audioData[2*i]) | int16(audioData[2*i+1])<<8
		sum += float64(val) * float64(val)
	}
	rms := math.Sqrt(sum / float64(n))

	const SeuilBruit = 15
	return rms < SeuilBruit
}

// DemarrerFluxMicro lance ffmpeg et retourne un ReadCloser vers le flux audio
func DemarrerFluxMicro(alsaDevice, windowsMic string) (io.ReadCloser, error) {
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		cmd = exec.Command("ffmpeg.exe",
			"-f", "dshow",
			"-i", "audio="+windowsMic,
			"-acodec", "pcm_s16le",
			"-ar", "16000",
			"-ac", "1",
			"-af", "highpass=f=200,volume=3.0",
			"-f", "wav",
			"pipe:1",
		)
	} else {
		cmd = exec.Command("ffmpeg",
			"-f", "alsa",
			"-i", alsaDevice,
			"-af", strings.Join([]string{
				"highpass=f=150",
				"afftdn=nr=20:tn=1",
				"compand=attacks=0.02:decays=0.1:points=-60/-40|-25/-15|-10/-10|0/-10:gain=5",
			}, ","),
			"-ar", "16000",
			"-ac", "1",
			"-f", "wav",
			"pipe:1",
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	fmt.Println("🎤 Micro activé en continu.")
	return stdout, nil
}

// SauvegarderAudio écrit un buffer audio dans un fichier
func SauvegarderAudio(data []byte, filename string) {
	import_os := func() {
		// placeholder — appelé via os.WriteFile dans le package main ou nlp
		log.Printf("SauvegarderAudio: %s (%d octets)", filename, len(data))
	}
	import_os()
}

// helpers binaires
func writeUint32(b *bytes.Buffer, v uint32) {
	b.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}
func writeUint16(b *bytes.Buffer, v uint16) {
	b.Write([]byte{byte(v), byte(v >> 8)})
}

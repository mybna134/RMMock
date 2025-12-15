package stream

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"gocv.io/x/gocv"
)

type X265Encoder struct {
	cfg        EncoderConfig
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	frameChan  chan []byte
	errChan    chan error
	headers    []byte
	frameCount uint64
	pts        int64

	mu        sync.Mutex
	headersMu sync.Mutex

	closed bool
}

func NewX265Encoder(cfg EncoderConfig) (*X265Encoder, error) {
	args := []string{
		"-f", "rawvideo",
		"-pixel_format", "bgr24",
		"-video_size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
		"-framerate", fmt.Sprintf("%.2f", cfg.FPS),
		"-i", "pipe:0",
		"-c:v", "libx265",
		"-pix_fmt", "yuv420p",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-x265-params", "repeat-headers=1:keyint=30:min-keyint=30:bframes=0:rc-lookahead=0",
		"-g", "30",
		"-flush_packets", "1",
		"-fflags", "+nobuffer+flush_packets",
	}

	if cfg.Bitrate > 0 {
		args = append(args, "-b:v", fmt.Sprintf("%dk", cfg.Bitrate))
	}
	if cfg.Preset != "" {
		args = append(args, "-preset", cfg.Preset)
	}
	if cfg.Tune != "" {
		args = append(args, "-tune", cfg.Tune)
	}
	if cfg.RepeatHeaders {
		args = append(args, "-x265-params", "repeat-headers=1")
	}

	args = append(args, "-f", "hevc", "-an", "pipe:1")

	cmd := exec.Command("ffmpeg", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w, %s", err, stderr.String())
	}

	enc := &X265Encoder{
		cfg:       cfg,
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		frameChan: make(chan []byte, 100),
		errChan:   make(chan error, 1),
	}

	// 🌟 异步读取 stdout
	go enc.captureOutput()

	return enc, nil
}

// 后台持续读取 stdout，不会阻塞 stdin
func (e *X265Encoder) captureOutput() {
	buf := make([]byte, 32*1024)

	for {
		n, err := e.stdout.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)

			// 保存 headers
			e.headersMu.Lock()
			if e.headers == nil {
				e.headers = append([]byte{}, data...)
			}
			e.headersMu.Unlock()

			// 将 HEVC NAL 包发送出去
			select {
			case e.frameChan <- data:
			default:
				// 如果满了就丢，避免阻塞
			}
		}

		if err != nil {
			e.errChan <- err
			close(e.frameChan)
			return
		}
	}
}

func (e *X265Encoder) GetHeaders() ([]byte, error) {
	e.headersMu.Lock()
	defer e.headersMu.Unlock()
	if e.headers != nil {
		return e.headers, nil
	}
	return nil, nil
}

// 写入帧 —— 始终不会阻塞
func (e *X265Encoder) EncodeFrame(mat gocv.Mat) ([]byte, error) {
	if mat.Empty() {
		return nil, fmt.Errorf("空帧")
	}
	
	e.mu.Lock()
	defer e.mu.Unlock()
	
	data := mat.ToBytes()
	expected := e.cfg.Width * e.cfg.Height * 3
	if len(data) != expected {
		return nil, fmt.Errorf("帧大小不匹配, 期望 %d, 实际 %d", expected, len(data))
	}
	
	_, err := e.stdin.Write(data)
	if err != nil {
		return nil, fmt.Errorf("写入失败: %w", err)
	}
	
	e.frameCount++
	e.pts++
	
	// 🔥 这里不阻塞，直接非阻塞读取 frameChan
	select {
	case out := <-e.frameChan:
		return out, nil
	default:
		return nil, nil
	}
}

// 刷出剩余数据
func (e *X265Encoder) Flush() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, nil
	}

	_ = e.stdin.Close()
	e.closed = true

	var out []byte
	for frame := range e.frameChan {
		out = append(out, frame...)
	}

	return out, nil
}

func (e *X265Encoder) Close() error {
	if !e.closed {
		_ = e.stdin.Close()
		e.closed = true
	}

	_ = e.stdout.Close()
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}

	return nil
}

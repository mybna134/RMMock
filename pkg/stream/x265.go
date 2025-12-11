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
	cfg       EncoderConfig
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	headers   []byte
	pts       int64
	mu        sync.Mutex
	headersMu sync.Mutex
	frameCount uint64
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
		// 关键参数: 每30帧一个GOP,立即输出
		"-x265-params", "repeat-headers=1:keyint=30:min-keyint=30:bframes=0:rc-lookahead=0",
		"-g", "30", // GOP大小
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
		// x265-params repeat-headers=1 会在每个I帧前重复VPS/SPS/PPS
		args = append(args, "-x265-params", "repeat-headers=1")
	}

	args = append(args,
		"-f", "hevc",
		"-an", // 禁用音频
		"pipe:1", // 输出到stdout
	)
	fmt.Printf("%v\n", args)

	cmd := exec.Command("ffmpeg", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("无法创建stdin管道: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("无法创建stdout管道: %w", err)
	}

	// 将stderr设置为缓冲区以捕获错误信息
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("无法启动ffmpeg: %w, stderr: %s", err, stderr.String())
	}

	encoder := &X265Encoder{
		cfg:    cfg,
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
	}

	return encoder, nil
}

func (e *X265Encoder) GetHeaders() ([]byte, error) {
	e.headersMu.Lock()
	defer e.headersMu.Unlock()

	if e.headers != nil {
		return e.headers, nil
	}

	// 对于HEVC,头部(VPS/SPS/PPS)通常在第一个输出包中
	// 由于我们使用repeat-headers=1,每个I帧都会包含头部
	// 这里我们返回空,让EncodeFrame处理
	return nil, nil
}

func (e *X265Encoder) EncodeFrame(mat gocv.Mat) ([]byte, error) {
	if mat.Empty() {
		return nil, fmt.Errorf("空帧")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 将BGR24数据写入ffmpeg的stdin
	data := mat.ToBytes()
	expectedSize := e.cfg.Width * e.cfg.Height * 3 // BGR24: 每像素3字节
println("fee")
	if len(data) != expectedSize {
		return nil, fmt.Errorf("帧大小不匹配: 期望 %d 字节, 实际 %d 字节", expectedSize, len(data))
	}
println("fefef")

	b, err := e.stdin.Write(data)
	println(b)
	if err != nil {
		return nil, fmt.Errorf("写入帧数据失败: %w", err)
	}
	println("ddd")

	e.frameCount++
	e.pts++
if e.frameCount < 30 {
		return nil, nil
	}
	// 读取编码后的数据
	// 注意: 这是一个简化实现,实际生产环境可能需要非阻塞读取
	buf := make([]byte, 1024) // 1MB缓冲区
	n, err := e.stdout.Read(buf)
	println("vdvd")
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("读取编码数据失败: %w", err)
	}

	if n == 0 {
		// 编码器缓冲中,没有输出
		return nil, nil
	}

	// 保存第一次的数据作为headers(如果还没有)
	e.headersMu.Lock()
	if e.headers == nil && n > 0 {
		// 通常前几个NAL单元是VPS/SPS/PPS
		e.headers = append([]byte{}, buf[:n]...)
	}
	e.headersMu.Unlock()

	return append([]byte{}, buf[:n]...), nil
}

func (e *X265Encoder) Flush() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 关闭stdin以通知ffmpeg输入结束
	e.stdin.Close()

	// 读取所有剩余数据
	var result []byte
	buf := make([]byte, 1024*1024)

	for {
		n, err := e.stdout.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("刷新读取失败: %w", err)
		}
	}

	return result, nil
}

func (e *X265Encoder) Close() error {
	if e.stdin != nil {
		e.stdin.Close()
	}
	if e.stdout != nil {
		e.stdout.Close()
	}
	if e.cmd != nil && e.cmd.Process != nil {
		e.cmd.Process.Kill()
		e.cmd.Wait()
	}
	return nil
}

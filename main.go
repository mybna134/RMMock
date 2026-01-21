package main

import (
	"os"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"
	"github.com/stydxm/RMMock/pkg/referee"
	"github.com/stydxm/RMMock/pkg/stream"
)

func main() {
	logrus.SetOutput(os.Stdout)
	//if os.Getenv("mode") == "dev" {
	logrus.SetLevel(logrus.DebugLevel)
	//}
	var wg sync.WaitGroup

	// 创建游戏模拟器 (3局2胜制)
	simulator := referee.NewGameSimulator(3)

	streamSource := 0
	wg.Add(1)
	// cha := make(chan gocv.Mat)
	cls := &atomic.Bool{}
	// 传递 simulator 给视频流,以便检查是否应该发送视频
	go stream.StartEncodedStream(streamSource, &wg, cls, simulator)

	// 启动 MQTT 服务器并传入 simulator
	go referee.StartMqttServer(&wg, simulator)

	// 启动发布循环并传入 simulator (包含控制台命令处理)
	go referee.Publish(simulator)

	// window := gocv.NewWindow("Encoded Camera Feed")
	// for !cls.Load() {
	// 	select {
	// 	case frame := <-cha:
	// 		window.IMShow(frame)
	// 		if window.WaitKey(1) >= 0 {
	// 			cls.Store(true)
	// 			break
	// 		}
	// 	default:
	// 	}
	// }
	logrus.Infof("退出程序中")
	wg.Wait()
	logrus.Infof("退出程序")
}

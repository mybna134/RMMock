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

	streamSource := 0
	wg.Add(1)
	// cha := make(chan gocv.Mat)
	cls := &atomic.Bool{}
	go stream.StartEncodedStream(streamSource, &wg, cls)

	go referee.StartMqttServer(&wg)
	go referee.Publish()
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

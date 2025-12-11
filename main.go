package main

import (
	"os"
	"sync"

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
	go stream.StartEncodedStream(streamSource, &wg)
	go referee.StartMqttServer(&wg)
	go referee.Publish()
println("IDFK")
	wg.Wait()
	logrus.Infof("退出程序")
}

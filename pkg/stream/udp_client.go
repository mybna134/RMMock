package stream

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/sirupsen/logrus"
)

func GetUDPConn() *net.UDPConn {
	// "172.17.0.3:3334"
	serverAddr, err := net.ResolveUDPAddr("udp", "172.17.0.2:3334")
	if err != nil {
		logrus.Fatalf("无法解析服务器地址: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		logrus.Fatalf("无法连接到UDP服务器: %v", err)
	}
	return conn
}

func PacketFactory(frameID, sliceID uint16, frameSize uint32,sliceData []byte) []byte {
	packet := make([]byte, 8+len(sliceData))

	binary.BigEndian.PutUint16(packet[0:2], frameID)
	binary.BigEndian.PutUint16(packet[2:4], sliceID)
	binary.BigEndian.PutUint32(packet[4:8], frameSize)
	copy(packet[8:], sliceData)

	return packet
}

func SendPacket(conn *net.UDPConn, encodedData []byte, frameID uint16) {
	// MTU 1500字节 - UDP头8字节 - IP头20字节 - 自定义头8字节 = 1464字节
	packetSize := 1400 // 安全值
	totalSlices := len(encodedData) / packetSize
	if len(encodedData)%packetSize != 0 {
		totalSlices++
	}

	// 发送每个切片
	for sliceID := uint16(0); sliceID < uint16(totalSlices); sliceID++ {
		start := int(sliceID) * packetSize
		end := min(start+packetSize, len(encodedData))

		// 通过UDP发送数据包到服务器
		packet := PacketFactory(frameID, sliceID, uint32(len(encodedData)), encodedData[start:end])
		_, err := conn.Write(packet)
		if err != nil {
			logrus.Errorf("发送切片 %d/%d 失败: %v", sliceID, totalSlices, err)
			continue
		}

		// 对大帧添加微小延迟避免拥塞
		if totalSlices > 10 {
			time.Sleep(50 * time.Microsecond)
		}
	}
}

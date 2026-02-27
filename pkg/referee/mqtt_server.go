package referee

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/stydxm/RMMock/pkg/rmcp"
	"google.golang.org/protobuf/proto"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
	sloglogrus "github.com/samber/slog-logrus/v2"
)

var server *mqtt.Server

// SubscribeDebugHook 订阅调试钩子
type SubscribeDebugHook struct {
	mqtt.HookBase
}

// ID 返回 hook 的唯一标识
func (h *SubscribeDebugHook) ID() string {
	return "subscribe-debug"
}

// Provides 声明这个 hook 提供的功能
func (h *SubscribeDebugHook) Provides(b byte) bool {
	return b == mqtt.OnSubscribe || b == mqtt.OnUnsubscribe
}

// OnSubscribe 当客户端订阅主题时触发
func (h *SubscribeDebugHook) OnSubscribe(cl *mqtt.Client, pk packets.Packet) packets.Packet {
	// logrus.Infof("[订阅调试] 客户端 '%s' 订阅主题:", cl.ID)
	// for _, sub := range pk.Filters {
	// 	logrus.Infof("  - Topic: %s, QoS: %d", sub.Filter, sub.Qos)
	// }
	return pk
}

// OnUnsubscribe 当客户端取消订阅主题时触发
func (h *SubscribeDebugHook) OnUnsubscribe(cl *mqtt.Client, pk packets.Packet) packets.Packet {
	// logrus.Infof("[订阅调试] 客户端 '%s' 取消订阅主题:", cl.ID)
	// for _, filter := range pk.Filters {
	// 	logrus.Infof("  - Topic: %s", filter.Filter)
	// }
	return pk
}

// PublishDebugHook 发布调试钩子
type PublishDebugHook struct {
	mqtt.HookBase
}

// ID 返回 hook 的唯一标识
func (h *PublishDebugHook) ID() string {
	return "publish-debug"
}

// Provides 声明这个 hook 提供的功能
func (h *PublishDebugHook) Provides(b byte) bool {
	return b == mqtt.OnPublish
}

// OnPublish 当消息发布时触发
func (h *PublishDebugHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	// 只打印 RobotInjuryStat 主题的调试信息

	if pk.TopicName == "Event" {
		// 反序列化消息以显示详细内容
		var msg rmcp.TechCoreMotionStateSync
		if err := proto.Unmarshal(pk.Payload, &msg); err == nil {
			logrus.Infof("[发布调试] TechCoreMotionStateSync - Status: %d, MaxDifficulty: %d, 数据大小: %d bytes",
				msg.Status, msg.MaximumDifficultyLevel, len(pk.Payload))
		} else {
			logrus.Warnf("[发布调试] TechCoreMotionStateSync 反序列化失败: %v, 数据大小: %d bytes", err, len(pk.Payload))
		}
	}
	return pk, nil
}

// HeroDeployModeCommandHook 部署模式指令处理钩子
type HeroDeployModeCommandHook struct {
	mqtt.HookBase
	simulator *GameSimulator
}

// ID 返回 hook 的唯一标识
func (h *HeroDeployModeCommandHook) ID() string {
	return "hero-deploy-mode-command"
}

// Provides 声明这个 hook 提供的功能
func (h *HeroDeployModeCommandHook) Provides(b byte) bool {
	return b == mqtt.OnPublished
}

// OnPublished 当消息发布完成后触发
func (h *HeroDeployModeCommandHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 仅处理 HeroDeployModeEventCommand 主题
	if pk.TopicName != "HeroDeployModeEventCommand" {
		return
	}

	// 反序列化消息
	var cmd rmcp.HeroDeployModeEventCommand
	if err := proto.Unmarshal(pk.Payload, &cmd); err != nil {
		logrus.Errorf("解析 HeroDeployModeEventCommand 失败: %v", err)
		return
	}

	logrus.Infof("收到部署模式指令: mode=%d", cmd.Mode)

	// 处理指令并检查是否需要立即发送响应
	shouldSendImmediately := h.simulator.handleDeployModeCommand(cmd.Mode)

	if shouldSendImmediately {
		// 立即发送 DeployModeStatusSync (仅用于 mode=0 退出部署模式)
		status := h.simulator.getDeployModeStatus()
		out, err := proto.Marshal(status)
		if err != nil {
			logrus.Errorf("序列化 DeployModeStatusSync 失败: %v", err)
			return
		}

		err = server.Publish("DeployModeStatusSync", out, false, 0)
		if err != nil {
			logrus.Errorf("发布 DeployModeStatusSync 失败: %v", err)
			return
		}

		logrus.Infof("已立即发送 DeployModeStatusSync: status=%d", status.Status)
	}
	// 对于 mode=1，会在 2 秒后由计时器自动发送
}

// AssemblyCommandHook 装配指令处理钩子
type AssemblyCommandHook struct {
	mqtt.HookBase
	simulator *GameSimulator
}

// ID 返回 hook 的唯一标识
func (h *AssemblyCommandHook) ID() string {
	return "assembly-command"
}

// Provides 声明这个 hook 提供的功能
func (h *AssemblyCommandHook) Provides(b byte) bool {
	return b == mqtt.OnPublished
}

// OnPublished 当消息发布完成后触发
func (h *AssemblyCommandHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 仅处理 AssemblyCommand 主题
	if pk.TopicName != "AssemblyCommand" {
		return
	}

	// 反序列化消息
	var cmd rmcp.AssemblyCommand
	if err := proto.Unmarshal(pk.Payload, &cmd); err != nil {
		logrus.Errorf("解析 AssemblyCommand 失败: %v", err)
		return
	}

	logrus.Infof("[能量单元装配] 收到装配指令: operation=%d, difficulty=%d", cmd.Operation, cmd.Difficulty)

	// 处理指令并检查是否需要立即发送响应
	shouldSendImmediately := h.simulator.handleAssemblyCommand(cmd.Operation, cmd.Difficulty)

	if shouldSendImmediately {
		// 立即发送 TechCoreMotionStateSync
		status := h.simulator.getTechCoreMotionStateSync()
		out, err := proto.Marshal(status)
		if err != nil {
			logrus.Errorf("序列化 TechCoreMotionStateSync 失败: %v", err)
			return
		}

		err = server.Publish("TechCoreMotionStateSync", out, false, 0)
		if err != nil {
			logrus.Errorf("发布 TechCoreMotionStateSync 失败: %v", err)
			return
		}

		// 详细的调试信息
		h.simulator.mu.RLock()
		difficulty := h.simulator.assemblyDifficulty
		h.simulator.mu.RUnlock()

		stateNames := map[uint32]string{
			0: "IDLE(空闲/未装配)",
			1: "IDLE(等待新装配)",
			2: "科技核心移动中",
			3: "科技核心到位,可进行首个装配步骤",
			4: "上一个装配步骤已完成,可进行下一个步骤",
			5: "装配步骤已全部完成,等待确认",
			6: "已确认装配,科技核心返回中",
		}
		stateName := stateNames[status.Status]

		logrus.Infof("[能量单元装配] 立即发送 TechCoreMotionStateSync: Status=%d (%s), MaxDifficulty=%d, 当前难度=%d",
			status.Status, stateName, status.MaximumDifficultyLevel, difficulty)
	}
}

// AirSupportCommandHook 空中支援指令处理钩子
type AirSupportCommandHook struct {
	mqtt.HookBase
	simulator *GameSimulator
}

// ID 返回 hook 的唯一标识
func (h *AirSupportCommandHook) ID() string {
	return "air-support-command"
}

// Provides 声明这个 hook 提供的功能
func (h *AirSupportCommandHook) Provides(b byte) bool {
	return b == mqtt.OnPublished
}

// OnPublished 当消息发布完成后触发
func (h *AirSupportCommandHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 仅处理 AirSupportCommand 主题
	if pk.TopicName != "AirSupportCommand" {
		return
	}

	// 反序列化消息
	var cmd rmcp.AirSupportCommand
	if err := proto.Unmarshal(pk.Payload, &cmd); err != nil {
		logrus.Errorf("解析 AirSupportCommand 失败: %v", err)
		return
	}

	logrus.Infof("收到空中支援指令: command_id=%d", cmd.CommandId)

	// 处理指令并检查是否需要立即发送响应
	shouldSendImmediately := h.simulator.handleAirSupportCommand(cmd.CommandId)

	if shouldSendImmediately {
		// 立即发送 AirSupportStatusSync
		status := h.simulator.getAirSupportStatusSync()
		out, err := proto.Marshal(status)
		if err != nil {
			logrus.Errorf("序列化 AirSupportStatusSync 失败: %v", err)
			return
		}

		err = server.Publish("AirSupportStatusSync", out, false, 0)
		if err != nil {
			logrus.Errorf("发布 AirSupportStatusSync 失败: %v", err)
			return
		}

		logrus.Infof("已发送 AirSupportStatusSync: status=%d", status.AirsupportStatus)
	}
}

// DartCommandHook 飞镖指令处理钩子
type DartCommandHook struct {
	mqtt.HookBase
	simulator *GameSimulator
}

// ID 返回 hook 的唯一标识
func (h *DartCommandHook) ID() string {
	return "dart-command"
}

// Provides 声明这个 hook 提供的功能
func (h *DartCommandHook) Provides(b byte) bool {
	return b == mqtt.OnPublished
}

// OnPublished 当消息发布完成后触发
func (h *DartCommandHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 仅处理 DartCommand 主题
	if pk.TopicName != "DartCommand" {
		return
	}

	// 反序列化消息
	var cmd rmcp.DartCommand
	if err := proto.Unmarshal(pk.Payload, &cmd); err != nil {
		logrus.Errorf("解析 DartCommand 失败: %v", err)
		return
	}

	logrus.Infof("收到飞镖指令: target_id=%d, open=%v", cmd.TargetId, cmd.Open)

	// 处理指令并检查是否需要立即发送响应
	shouldSendImmediately := h.simulator.handleDartCommand(cmd.TargetId, cmd.Open)

	if shouldSendImmediately {
		// 立即发送 DartSelectTargetStatusSync
		status := h.simulator.getDartStatus()
		out, err := proto.Marshal(status)
		if err != nil {
			logrus.Errorf("序列化 DartSelectTargetStatusSync 失败: %v", err)
			return
		}

		err = server.Publish("DartSelectTargetStatusSync", out, false, 0)
		if err != nil {
			logrus.Errorf("发布 DartSelectTargetStatusSync 失败: %v", err)
			return
		}

		logrus.Infof("已立即发送 DartSelectTargetStatusSync: target_id=%d, open=%v", status.TargetId, status.Open)
	}
}

// RuneActivateCommandHook 能量机关激活指令处理钩子
type RuneActivateCommandHook struct {
	mqtt.HookBase
	simulator *GameSimulator
}

// RobotPerformanceSelectionHook 性能体系选择指令处理钩子
type RobotPerformanceSelectionHook struct {
	mqtt.HookBase
	simulator *GameSimulator
}

type CommonCommandHook struct {
	mqtt.HookBase
	simulator *GameSimulator
}

// ID 返回 hook 的唯一标识
func (h *RuneActivateCommandHook) ID() string {
	return "rune-activate-command"
}

// Provides 声明这个 hook 提供的功能
func (h *RuneActivateCommandHook) Provides(b byte) bool {
	return b == mqtt.OnPublished
}

// OnPublished 当消息发布完成后触发
func (h *RuneActivateCommandHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 仅处理 RuneActivateCommand 主题
	if pk.TopicName != "RuneActivateCommand" {
		return
	}

	// 反序列化消息
	var cmd rmcp.RuneActivateCommand
	if err := proto.Unmarshal(pk.Payload, &cmd); err != nil {
		logrus.Errorf("解析 RuneActivateCommand 失败: %v", err)
		return
	}

	logrus.Infof("收到能量机关激活指令: activate=%d", cmd.Activate)

	// 处理指令并检查是否需要立即发送响应
	shouldSendImmediately := h.simulator.handleRuneActivateCommand(cmd.Activate)

	if shouldSendImmediately {
		// 立即发送 RuneStatusSync
		status := h.simulator.getRuneStatusSync()
		out, err := proto.Marshal(status)
		if err != nil {
			logrus.Errorf("序列化 RuneStatusSync 失败: %v", err)
			return
		}

		err = server.Publish("RuneStatusSync", out, false, 0)
		if err != nil {
			logrus.Errorf("发布 RuneStatusSync 失败: %v", err)
			return
		}

		logrus.Infof("已立即发送 RuneStatusSync: status=%d, arms=%d, avgRings=%d",
			status.RuneStatus, status.ActivatedArms, status.AverageRings)
	}
}

func StartMqttServer(wg *sync.WaitGroup, simulator *GameSimulator) {
	defer wg.Done()

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	slogLogger := slog.New(sloglogrus.Option{Logger: logrus.New()}.NewLogrusHandler())
	server = mqtt.New(&mqtt.Options{Logger: slogLogger, InlineClient: true})
	_ = server.AddHook(new(auth.AllowHook), nil)
	_ = server.AddHook(new(SubscribeDebugHook), nil)
	_ = server.AddHook(new(PublishDebugHook), nil)
	_ = server.AddHook(&HeroDeployModeCommandHook{simulator: simulator}, nil)
	_ = server.AddHook(&AssemblyCommandHook{simulator: simulator}, nil)
	_ = server.AddHook(&AirSupportCommandHook{simulator: simulator}, nil)
	_ = server.AddHook(&DartCommandHook{simulator: simulator}, nil)
	_ = server.AddHook(&RuneActivateCommandHook{simulator: simulator}, nil)
	_ = server.AddHook(&RobotPerformanceSelectionHook{simulator: simulator}, nil)
	_ = server.AddHook(&CommonCommandHook{simulator: simulator}, nil)
	tcp := listeners.NewTCP(listeners.Config{ID: "t1", Address: ":3333"})
	err := server.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		err := server.Serve()
		if err != nil {
			log.Fatal(err)
		}
	}()

	<-done
}

// ID 返回 RobotPerformanceSelectionHook 的唯一标识
func (h *RobotPerformanceSelectionHook) ID() string {
	return "robot-performance-selection"
}

// Provides 声明这个 hook 提供的功能
func (h *RobotPerformanceSelectionHook) Provides(b byte) bool {
	return b == mqtt.OnPublished
}

// OnPublished 当消息发布完成后触发
func (h *RobotPerformanceSelectionHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 仅处理 RobotPerformanceSelectionCommand 主题
	if pk.TopicName != "RobotPerformanceSelectionCommand" {
		return
	}

	// 反序列化消息
	var cmd rmcp.RobotPerformanceSelectionCommand
	if err := proto.Unmarshal(pk.Payload, &cmd); err != nil {
		logrus.Errorf("解析 RobotPerformanceSelectionCommand 失败: %v", err)
		return
	}

	logrus.Infof("收到性能体系选择指令: shooter=%d, chassis=%d, sentry_control=%d",
		cmd.Shooter, cmd.Chassis, cmd.SentryControl)

	// 处理指令并检查是否需要立即发送响应
	shouldSendImmediately := h.simulator.handleRobotPerformanceSelectionCommand(cmd.Shooter, cmd.Chassis, cmd.SentryControl)

	if shouldSendImmediately {
		// 立即发送 RobotPerformanceSelectionSync
		status := h.simulator.getRobotPerformanceSelectionSync()
		out, err := proto.Marshal(status)
		if err != nil {
			logrus.Errorf("序列化 RobotPerformanceSelectionSync 失败: %v", err)
			return
		}

		err = server.Publish("RobotPerformanceSelectionSync", out, false, 0)
		if err != nil {
			logrus.Errorf("发布 RobotPerformanceSelectionSync 失败: %v", err)
			return
		}

		logrus.Infof("已发送 RobotPerformanceSelectionSync: shooter=%d, chassis=%d",
			status.Shooter, status.Chassis)
	}
}

// ID 返回 CommonCommandHook 的唯一标识
func (h *CommonCommandHook) ID() string {
	return "common-command"
}

// Provides 声明这个 hook 提供的功能
func (h *CommonCommandHook) Provides(b byte) bool {
	return b == mqtt.OnPublished
}

// OnPublished 当消息发布完成后触发
func (h *CommonCommandHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 仅处理 CommonCommand 主题
	if pk.TopicName != "CommonCommand" {
		return
	}

	// 反序列化消息
	var cmd rmcp.CommonCommand
	if err := proto.Unmarshal(pk.Payload, &cmd); err != nil {
		logrus.Errorf("解析 CommonCommand 失败: %v", err)
		return
	}

	logrus.Infof("收到通用指令: cmd_type=%d, param=%d", cmd.CmdType, cmd.Param)

	// 处理指令
	h.simulator.handleCommonCommand(cmd.CmdType, cmd.Param)
}

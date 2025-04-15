package communication

import (
	"context"
	"fmt"
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/ipv4"
)

// UDPConfig 配置结构体
type UDPConfig struct {
	Port          int // 监听端口
	BufferSize    int // 接收缓冲区大小
	ChannelSize   int // 管道缓冲区大小
	Logger        *log.Logger
	MulticastIP   string   // 组播地址（可选）
	SourceIPs     []string // 组播源地址（可选，支持多个）
	InterfaceName string   // 网络接口名称（可选）
}

// SendCommand 发送命令结构
type SendCommand struct {
	Data []byte // 要发送的数据
	Addr string // 目标地址（格式：ip:port）
}

// UDPServer UDP服务器
type UDPServer struct {
	config     UDPConfig
	sendChan   chan SendCommand
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	conn       *net.UDPConn
	running    int32 // 是否正在运行,0-未运行,1-正在运行
	handler    func(*net.UDPAddr, []byte) []byte
	withSender bool
}

func (r *UDPServer) listInterface() {
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("Failed to list interfaces: %v", err)
	}
	for _, iface := range interfaces {
		fmt.Printf("Name: %s, Flags: %s\n", iface.Name, iface.Flags.String())
	}
}

// Start 启动UDP服务器
func (r *UDPServer) StartUDPServer(config UDPConfig, handler func(*net.UDPAddr, []byte) []byte, withSender bool) {
	r.config = config

	// 设置默认 Logger
	if r.config.Logger == nil {
		r.config.Logger = log.Default()
	}

	// 检查端口号是否合法
	if r.config.Port <= 0 || r.config.Port > 65535 {
		r.logError("Invalid port: %d. Port must be between 1 and 65535.", r.config.Port)
		return
	}

	// 检查是否已经在运行
	if atomic.LoadInt32(&r.running) == 1 || r.conn != nil {
		r.logWarn("UDP server is already running.")
		return
	}

	// 设置默认值
	if r.config.BufferSize <= 0 {
		r.config.BufferSize = 1024 // 默认缓冲区大小
		r.logInfo("BufferSize not set. Using default: %d bytes.", r.config.BufferSize)
	}
	if r.config.ChannelSize <= 0 {
		r.config.ChannelSize = 1024 // 默认管道大小
		r.logInfo("ChannelSize not set. Using default: %d.", r.config.ChannelSize)
	}

	r.listInterface()
	// 解析地址
	var localAddr *net.UDPAddr
	var err error
	if r.config.MulticastIP != "" {
		// 如果配置了组播地址
		localAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", r.config.MulticastIP, r.config.Port))
	} else {
		// 普通UDP监听
		localAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", r.config.Port))
	}
	if err != nil {
		r.logError("Resolve UDP address failed: %v", err)
		return
	}

	// 监听UDP
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		r.logError("Listen UDP failed: %v", err)
		return
	}
	r.conn = conn

	// 如果是组播，加入组播组
	if r.config.MulticastIP != "" {
		err := r.joinSourceSpecificMulticastGroup(r.config.MulticastIP, r.config.SourceIPs, r.config.InterfaceName)
		if err != nil {
			r.logError("Join multicast group failed: %v", err)
			return
		}
		r.logInfo("Joined multicast group: %s", r.config.MulticastIP)
	}

	// 初始化其他参数
	r.sendChan = make(chan SendCommand, r.config.ChannelSize)
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.handler = handler
	r.withSender = withSender
	r.logInfo("Listening on %s", conn.LocalAddr().String())
	r.logInfo("Buffer size: %d bytes", r.config.BufferSize)
	r.logInfo("Channel size: %d", r.config.ChannelSize)
	atomic.StoreInt32(&r.running, 1)
	r.wg.Add(1)
	go r.run()
	if r.withSender {
		r.wg.Add(1)
		go r.startSender()
	}
}

// joinMulticastGroup 加入组播组
func (r *UDPServer) joinMulticastGroup(multicastIP string, interfaceName string) error {
	// 解析组播地址
	group := net.ParseIP(multicastIP)
	if group == nil {
		return fmt.Errorf("无效的组播地址: %s", multicastIP)
	}

	// 获取指定的网络接口
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return fmt.Errorf("获取网络接口失败: %w", err)
	}
	if iface == nil {
		return fmt.Errorf("未找到网络接口: %s", interfaceName)
	}

	// 使用 ipv4 包加入组播组
	p := ipv4.NewPacketConn(r.conn)
	err = p.JoinGroup(iface, &net.UDPAddr{IP: group})
	if err != nil {
		return fmt.Errorf("加入组播组失败: %w", err)
	}
	return nil
}

// Stop 停止UDP服务器
func (r *UDPServer) StopUDPServer() {
	if r.conn != nil {
		if r.cancel != nil {
			r.cancel() // 只有在 r.cancel 不为 nil 时调用
		}
		r.wg.Wait()
		atomic.StoreInt32(&r.running, 0)
		if err := r.conn.Close(); err != nil {
			r.logError("Close UDP connection failed: %v", err)
		}
		r.conn = nil
		r.logInfo("UDP server stopped")
	}
}

// SendData 发送数据
func (r *UDPServer) SendData(data []byte, addr string) {
	select {
	case r.sendChan <- SendCommand{Data: data, Addr: addr}:
		// 入队成功
	case <-r.ctx.Done():
		// 已关闭
	default:
		r.logWarn("Send channel full, message to %s dropped", addr)
	}
}

// startSender 启动发送器
func (r *UDPServer) startSender() {
	defer r.wg.Done()
	defer close(r.sendChan)
	for {
		select {
		case <-r.ctx.Done():
			r.logInfo("UDP sender stopping...")
			return
		case cmd := <-r.sendChan:
			r.send(cmd)
		}
	}
}

// send 发送数据
func (r *UDPServer) send(cmd SendCommand) {
	remoteAddr, err := net.ResolveUDPAddr("udp", cmd.Addr)
	if err != nil {
		r.logError("Resolve address failed: %v", err)
		return
	}

	_, err = r.conn.WriteToUDP(cmd.Data, remoteAddr)
	if err != nil {
		r.logError("Send UDP data failed: %v", err)
	}
}

// run 接收器主循环
func (r *UDPServer) run() {
	defer r.wg.Done()

	buffer := make([]byte, r.config.BufferSize)

	for {
		select {
		case <-r.ctx.Done():
			r.logInfo("UDP receiver stopping...")
			return
		default:
			if err := r.conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
				r.logError("Set read deadline error: %v", err)
				return
			}
			n, addr, err := r.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				r.logError("Read error: %v", err)
				return
			}
			if n == 0 {
				continue
			}

			if r.handler != nil {
				go func(addr *net.UDPAddr, data []byte) {
					response := r.handler(addr, data)
					if len(response) > 0 {
						_, err := r.conn.WriteToUDP(response, addr)
						if err != nil {
							r.logError("Send response error: %v", err)
						}
					}
				}(addr, buffer[:n])
			}
		}
	}
}

// logInfo 打印信息日志
func (r *UDPServer) logInfo(format string, v ...interface{}) {
	if r.config.Logger != nil {
		r.config.Logger.Printf("[INFO] "+format, v...)
	}
}

// logWarn 打印警告日志
func (r *UDPServer) logWarn(format string, v ...interface{}) {
	if r.config.Logger != nil {
		r.config.Logger.Printf("[WARN] "+format, v...)
	}
}

// logError 打印错误日志
func (r *UDPServer) logError(format string, v ...interface{}) {
	if r.config.Logger != nil {
		r.config.Logger.Printf("[ERROR] "+format, v...)
	}
}

// SendMulticast 发送组播消息
func SendMulticast(multicastIP string, port int, message string) error {
	// 解析组播地址
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", multicastIP, port))
	if err != nil {
		return fmt.Errorf("解析组播地址失败: %w", err)
	}

	// 创建UDP连接
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("创建UDP连接失败: %w", err)
	}
	defer conn.Close()

	_, errw := conn.Write([]byte(message))
	if errw != nil {
		return fmt.Errorf("发送组播消息失败: %w", errw)
	}

	return nil
}

func (r *UDPServer) joinSourceSpecificMulticastGroup(multicastIP string, sourceIPs []string, interfaceName string) error {
	// 检查是否运行在 Windows 平台
	if runtime.GOOS == "windows" {
		r.logWarn("Source-specific multicast is not supported on Windows. Falling back to normal multicast.")
		return r.joinMulticastGroup(multicastIP, interfaceName)
	}

	// 解析组播地址
	group := net.ParseIP(multicastIP)
	if group == nil {
		return fmt.Errorf("无效的组播地址: %s", multicastIP)
	}

	// 获取指定的网络接口
	var iface *net.Interface
	var err error
	if interfaceName != "" {
		iface, err = net.InterfaceByName(interfaceName)
		if err != nil {
			return fmt.Errorf("获取网络接口失败: %w", err)
		}
	}

	// 如果没有指定组播源，调用普通组播加入逻辑
	if len(sourceIPs) == 0 {
		r.logInfo("No source IPs specified, joining multicast group without source filtering.")
		return r.joinMulticastGroup(multicastIP, interfaceName)
	}

	// 使用 ipv4 包加入每个组播源
	p := ipv4.NewPacketConn(r.conn)
	for _, sourceIP := range sourceIPs {
		source := net.ParseIP(sourceIP)
		if source == nil {
			return fmt.Errorf("无效的组播源地址: %s", sourceIP)
		}

		err = p.JoinSourceSpecificGroup(iface, &net.UDPAddr{IP: group}, &net.UDPAddr{IP: source})
		if err != nil {
			return fmt.Errorf("加入指定源组播组失败 (组播地址: %s, 源地址: %s): %w", multicastIP, sourceIP, err)
		}
		r.logInfo("Joined source-specific multicast group: %s (source: %s)", multicastIP, sourceIP)
	}

	return nil
}

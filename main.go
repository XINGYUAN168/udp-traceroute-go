// student-style-comments.go

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	// 引入 Go 官方的扩展网络库，用于处理更底层的 ICMP 和 IPv4 协议
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func main() {
	// 程序的入口点，首先处理命令行参数
	// 检查用户是否在命令行提供了目标地址
	if len(os.Args) < 2 {
		// 如果没有提供，就打印用法提示并退出程序
		log.Fatalf("用法: sudo go run main.go <目标地址>")
	}
	// os.Args[0] 是程序名, os.Args[1] 是第一个参数
	target := os.Args[1]

	// 将用户提供的域名或IP字符串，解析为标准的IP地址结构
	destIPAddr, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		log.Fatalf("错误：无法将 '%s' 解析为有效的IPv4地址: %v", target, err)
	}
	// 从解析结果中提取出IP地址备用
	destIP := destIPAddr.IP

	fmt.Printf("开始 traceroute 到 %s (%s)\n", target, destIP.String())

	// 准备一个专门用来接收ICMP返回包的连接。
	// traceroute的原理就是发送UDP包并监听ICMP错误，所以收发是分离的。
	// "ip4:icmp" 表示监听IPv4协议中的所有ICMP类型的包。
	// "0.0.0.0" 表示监听本机所有网络接口。
	icmpConn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		log.Fatalf("错误：创建ICMP监听连接失败: %v", err)
	}
	// 使用defer确保在main函数结束时，这个连接一定会被关闭，以释放系统资源。
	defer icmpConn.Close()

	// 定义traceroute过程中的一些常量和变量
	const maxHops = 30              // 设置最大探测跳数，防止无限循环
	const timeout = 2 * time.Second // 为每一跳设置2秒的超时时间
	destPort := 33434               // 选择一个不常用的高位端口作为UDP探测包的目标端口

	// 核心探测逻辑：通过一个循环来逐步增加TTL值
	for ttl := 1; ttl <= maxHops; ttl++ {
		// 打印当前正在探测的跳数
		fmt.Printf("%2d ", ttl)

		// 为本次探测创建一个专用的UDP发送连接
		// 监听 "0.0.0.0:0" 表示让操作系统在所有网络接口上为我们选择一个随机的可用端口
		sendSocket, err := net.ListenPacket("udp4", "0.0.0.0:0")
		if err != nil {
			log.Fatalf("错误：创建UDP发送连接失败: %v", err)
		}

		// 这里是对TCP示例模仿的关键：
		// 1. 将标准的 net.PacketConn 包装成 ipv4.PacketConn
		// 2. 这样我们就能获得对IP协议头部的控制权，特别是设置TTL
		p := ipv4.NewPacketConn(sendSocket)
		if err := p.SetTTL(ttl); err != nil {
			log.Fatalf("错误：设置TTL为 %d 失败: %v", ttl, err)
		}
		// 每个循环创建的发送连接在循环结束时都应该关闭
		defer p.Close()

		// 定义UDP包的目标地址，包含IP和端口
		udpAddr := &net.UDPAddr{IP: destIP, Port: destPort}

		// 发送探测包。内容为空，因为我们只关心IP头和UDP头。
		if _, err := p.WriteTo([]byte(""), nil, udpAddr); err != nil {
			log.Fatalf("错误：发送UDP探测包失败: %v", err)
		}

		// ---- 发送完成，现在开始等待回应 ----

		// 创建一个足够大的字节切片作为缓冲区，用来接收返回的ICMP包
		replyBytes := make([]byte, 1500)
		// 为本次接收操作设置一个超时期限
		icmpConn.SetReadDeadline(time.Now().Add(timeout))

		// 阻塞式读取ICMP连接，直到收到数据包或超时
		_, peerAddr, err := icmpConn.ReadFrom(replyBytes)
		if err != nil {
			// 如果错误是网络超时错误，说明这一跳的路由器没有回应
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Println("* * * Request timed out.")
				continue // 继续下一次循环，探测下一跳
			}
			// 如果是其他错误，打印日志
			log.Printf("读取ICMP回应时出错: %v\n", err)
			continue
		}

		// 将收到的原始字节流解析成结构化的ICMP消息
		// 协议号 "1" 代表 ICMPv4
		icmpMessage, err := icmp.ParseMessage(1, replyBytes)
		if err != nil {
			log.Printf("解析ICMP消息时出错: %v\n", err)
			continue
		}

		// 分析ICMP消息的类型，判断当前探测的状态
		// peerAddr 是返回ICMP消息的主机IP地址，即当前这一跳的路由器地址
		fmt.Printf("%-15s ", peerAddr.String())
		switch icmpMessage.Type {
		case ipv4.ICMPTypeTimeExceeded:
			// 类型11: Time Exceeded (超时)
			// 这是我们期望从中间路由器收到的回复，表示探测包的TTL已耗尽
			fmt.Println("(Time Exceeded)")
		case ipv4.ICMPTypeDestinationUnreachable:
			// 类型3: Destination Unreachable (目标不可达)
			// 这通常是最终目标主机返回的，因为我们的UDP包到达了一个未被监听的端口
			// 这标志着traceroute过程的成功结束
			fmt.Println("(Destination Unreachable)")
			fmt.Println("Traceroute 完成!")
			return // 成功到达终点，退出程序
		default:
			// 如果收到其他类型的ICMP包，也打印出来以供分析
			fmt.Printf("(未知 ICMP 类型: %d)\n", icmpMessage.Type)
		}
	}
}

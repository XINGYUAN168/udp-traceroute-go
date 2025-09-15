// main.go (正确模仿TCP示例的版本)

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	// 明确使用官方扩展库来处理ICMP和IPv4
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func main() {
	// --- 1. 定义目标 ---
	if len(os.Args) < 2 {
		log.Fatalf("用法: sudo go run main.go <目标地址>")
	}
	target := os.Args[1]

	destIPAddr, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		log.Fatalf("无法解析目标地址: %v", err)
	}
	destIP := destIPAddr.IP

	fmt.Printf("开始 traceroute 到 %s (%s)\n", target, destIP.String())

	// --- 2. 创建用于接收 ICMP 回复的连接 ---
	// 这是专门用来“听”ICMP错误的连接，与发送UDP的连接是分开的
	icmpConn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		log.Fatalf("监听 ICMP 失败: %v", err)
	}
	defer icmpConn.Close()

	// --- 3. 主循环与参数设置 ---
	const maxHops = 30
	const timeout = 2 * time.Second
	destPort := 33434 // 我们发送UDP包的目标端口

	for ttl := 1; ttl <= maxHops; ttl++ {
		fmt.Printf("%2d ", ttl)

		// --- 4. 构造并发送UDP探测包 ---
		// 这部分的实现方式，正是对您同学TCP例子的模仿

		// 1. 创建一个底层的UDP PacketConn 连接
		sendSocket, err := net.ListenPacket("udp4", "0.0.0.0:0") // 监听一个随机的本地端口
		if err != nil {
			log.Fatalf("创建发送套接字失败: %v", err)
		}

		// 2. 将这个连接包装成一个 ipv4.PacketConn 对象，从而获得控制IP头的能力
		p := ipv4.NewPacketConn(sendSocket)
		// 3. 设置我们最关心的TTL值
		if err := p.SetTTL(ttl); err != nil {
			log.Fatalf("设置 TTL 失败: %v", err)
		}
		// 函数退出时自动关闭这个发送连接
		defer p.Close()

		// 4. 构造一个包含IP和Port的目标地址
		udpAddr := &net.UDPAddr{IP: destIP, Port: destPort}

		// 5. 发送一个空的UDP包到目标地址
		_, err = p.WriteTo([]byte(""), nil, udpAddr)
		if err != nil {
			log.Fatalf("发送 UDP 包失败: %v", err)
		}

		// --- 5. 在ICMP连接上等待并解析回应 ---
		replyBytes := make([]byte, 1500)
		icmpConn.SetReadDeadline(time.Now().Add(timeout))

		n, peerAddr, err := icmpConn.ReadFrom(replyBytes)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Println("* * * Request timed out.")
				continue
			}
			log.Printf("读取 ICMP 失败: %v\n", err)
			continue
		}

		// 解析收到的ICMP消息
		icmpMessage, err := icmp.ParseMessage(1, replyBytes[:n]) // 1 for ICMPv4
		if err != nil {
			log.Printf("解析 ICMP 消息失败: %v\n", err)
			continue
		}

		// --- 6. 判断ICMP消息类型 ---
		fmt.Printf("%-15s ", peerAddr.String())
		switch icmpMessage.Type {
		case ipv4.ICMPTypeTimeExceeded:
			fmt.Println("(Time Exceeded)")
		case ipv4.ICMPTypeDestinationUnreachable:
			fmt.Println("(Destination Unreachable)")
			fmt.Println("Traceroute 完成!")
			return
		default:
			fmt.Printf("(未知 ICMP 类型: %d)\n", icmpMessage.Type)
		}
	}
}

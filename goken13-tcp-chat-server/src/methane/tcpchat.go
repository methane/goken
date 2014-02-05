package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"sync"
)

const BUFSIZE = 64

type Message struct {
	sender  string
	message string
}

type Client struct {
	name   string
	conn   net.Conn
	reader *bufio.Reader
	ch     chan string
}

type Room struct {
	sync.Mutex // mixin
	clients    map[string]*Client
	recvCh     chan *Message
}

func NewRoom() *Room {
	// Mutex は初期化不要
	// ゼロ値が unlocked な mutex
	room := &Room{
		clients: make(map[string]*Client),
		recvCh:  make(chan *Message),
	}

	// さきに broadcast だけ回す
	go func() { // broadcast
		for {
			msg := <-room.recvCh
			text := fmt.Sprintf("%s: %s", msg.sender, msg.message)
			fmt.Println(text)
			room.Lock()
			// broadcast 中に room から離脱とかあるとマズイので
			// ここでロックを取る。
			for _, c := range room.clients {
				// bufsize までしか書き込めず、いっぱいになるとスルーされる
				// これによりクライアントの遅延により全体に影響することがない。
				select {
				case c.ch <- text:
				default: // TODO: default はなんで必要なんだっけ？
				}
			}
			room.Unlock()
		}
	}()

	return room
}

func (room *Room) Join(conn net.Conn) {
	client := NewClient(conn, room)

	// room.clients は map であり、スレッドセーフではないので
	// ここでロックを取る。
	// broadcast 中は clients には追加されず待たされる
	room.Lock()
	room.clients[client.name] = client
	room.Unlock()

	// client を追加してからループを回すようにする。
	// 理由は .............................?
	fmt.Println("joined: ", client.name)
	client.Start(room)
}

func (room *Room) Apart(name string) {
	// ここは関数全体でロックをとれるので
	// defer で unlock している。
	room.Lock()
	defer room.Unlock()
	// ここは close もするけど、もし delete だけであれば
	// map の中を確認せず delete しても良い
	client, ok := room.clients[name]
	if ok {
		delete(room.clients, name)
		close(client.ch)
	}
}

func NewClient(c net.Conn, room *Room) *Client {
	reader := bufio.NewReader(c)
	c.Write([]byte("Your name?> "))
	name, err := reader.ReadString('\n')
	if err != nil {
		return nil
	}
	log.Printf("name: %q\n", name)
	name = name[:len(name)-2] // len(name) - 1 だと \r になって消えてるっぽい
	return &Client{
		name:   name,
		conn:   c,
		reader: reader,
		ch:     make(chan string, BUFSIZE)} // BUFSIZE までの chan にしておく
	// TODO: こうやって書くのは末尾のカンマを無くしたいから？
}

func (client *Client) Start(room *Room) {
	go func() { // sender

		// 先に Close を書くと Close 中に panic になったものが
		// recover できないので先に revocer を書く。
		// defer は後に書かれた方から実行される。
		defer func() {
			if r := recover(); r != nil {
				fmt.Println(r)
			}
		}()
		defer client.conn.Close()

		for {
			msg, ok := <-client.ch
			if !ok {
				return
			}
			_, err := client.conn.Write([]byte(msg))
			if err != nil {
				// TODO: 書き込めないということはクライアントが落ちてる
				// このときどうやって receiver 止めるか？
				return
			}
		}
	}()

	go func() { // receiver
		defer func() {
			if r := recover(); r != nil {
				fmt.Println(r)
			}
		}()
		for {
			read, err := client.reader.ReadString('\n')
			log.Printf("%q\n", read)
			if err != nil {
				room.Apart(client.name)
				break
			}
			fmt.Printf("name: %q\nmessage: %q\n", client.name, read)
			message := &Message{
				sender:  client.name,
				message: read,
			}
			room.recvCh <- message
		}
	}()
}

// TODO: これ使ってる？
func (client *Client) Send(msg string) bool {
	select {
	case client.ch <- msg:
		return true
	default:
		close(client.ch)
		return false
	}
}

func main() {
	room := NewRoom()

	listener, err := net.Listen("tcp", ":3000")
	if err != nil {
		log.Panic(err)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Panic(err)
		}
		go room.Join(conn)
	}
}

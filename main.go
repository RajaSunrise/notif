package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

// Channel untuk mengirim pesan ke klien yang terhubung
var clients = make(map[chan string]bool)
var newClients = make(chan chan string)
var closedClients = make(chan chan string)
var messages = make(chan string)

func main() {
	app := fiber.New()

	// Middleware CORS untuk mengizinkan permintaan dari browser
	app.Use(cors.New())

	// Route untuk menyajikan file statis (index.html)
	app.Static("/", "./static")

	// Handler untuk menerima SSE
	app.Get("/events", func(c *fiber.Ctx) error {
		// Mengatur header untuk Server-Sent Events
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("Access-Control-Allow-Origin", "*") // Penting untuk CORS jika klien beda domain

		// Optional: Beberapa server proxy (seperti Nginx) mungkin mem-buffer output,
		// yang bisa menunda pengiriman SSE. Header ini dapat membantu.
		c.Set("X-Accel-Buffering", "no")

		// Membuat channel baru untuk klien ini
		clientChan := make(chan string)
		newClients <- clientChan // Mendaftarkan klien baru

		log.Println("Client connected, waiting for messages...")

		// Loop untuk mengirim pesan ke klien
		for {
			select {
			case msg := <-clientChan:
				// Mengirim pesan dalam format SSE
				// Format: data: [pesan]\n\n
				_, err := c.Write([]byte(fmt.Sprintf("data: %s\n\n", msg)))
				if err != nil {
					log.Printf("Error sending message to client: %v", err)
					// Jika ada error saat menulis, asumsikan klien terputus dan keluar dari loop
					return err
				}

			case <-c.Context().Done(): // Deteksi penutupan koneksi dari klien
				closedClients <- clientChan // Beri tahu goroutine manajemen untuk menghapus klien
				log.Println("Client context done, assuming disconnected")
				return nil // Keluar dari handler
			}
		}
	})

	// Route untuk memicu pengiriman notifikasi (simulasi admin/sistem lain)
	app.Post("/send-notification", func(c *fiber.Ctx) error {
		type Notification struct {
			Message string `json:"message"`
		}
		var notif Notification
		if err := c.BodyParser(&notif); err != nil { // TIDAK ADA KARAKTER '¬' DI SINI!
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
		}

		if notif.Message == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Message cannot be empty"})
		}

		messages <- notif.Message // Mengirim pesan ke channel global
		return c.JSON(fiber.Map{"status": "Notification sent to all connected clients"})
	})

	// Goroutine untuk mengelola klien dan mengirim pesan
	go func() {
		for {
			select {
			case s := <-newClients:
				clients[s] = true
				log.Printf("New client connected, total clients: %d", len(clients))
			case s := <-closedClients:
				if _, ok := clients[s]; ok { // Pastikan klien masih ada sebelum dihapus
					delete(clients, s)
					close(s) // Penting: tutup channel setelah dihapus
					log.Printf("Client disconnected, total clients: %d", len(clients))
				}
			case msg := <-messages:
				log.Printf("Broadcasting message: %s", msg)
				for clientChan := range clients {
					// Menggunakan select dengan default untuk menghindari blocking
					// jika channel klien penuh atau sudah ditutup (tapi belum dihapus)
					select {
					case clientChan <- msg: // Kirim pesan ke klien
					default:
						// Jika klien tidak bisa menerima pesan (buffer penuh, atau sudah disconnect secara tidak terduga)
						log.Printf("Client channel full or closed, removing it: %v", clientChan)
						if _, ok := clients[clientChan]; ok {
							delete(clients, clientChan) // Hapus klien yang bermasalah
							close(clientChan)
						}
					}
				}
			}
		}
	}()

	// Goroutine untuk mengirim notifikasi otomatis setiap 5 detik (opsional)
	go func() {
		counter := 0
		for {
			time.Sleep(5 * time.Second)
			counter++
			messages <- fmt.Sprintf("Automatic server update #%d", counter)
		}
	}()

	log.Fatal(app.Listen(":3000"))
}

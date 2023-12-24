package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/websocket/v2"
)

//go:embed assets
var assets embed.FS

func main() {
	var port int
	var username, password string
	var docker bool
	var user bool

	flag.IntVar(&port, "p", 8000, "Server port number")
	flag.StringVar(&username, "au", "", "Username for basic auth")
	flag.StringVar(&password, "ap", "", "Password for basic auth")
	flag.BoolVar(&docker, "docker", false, "Add container names for Docker scopes (with journald logging driver)")
	flag.BoolVar(&user, "u", false, "User logs")
	flag.Parse()

	app := fiber.New(fiber.Config{
		Prefork: false,
	})

	app.Use(recover.New())
	app.Use(logger.New())

	if username != "" || password != "" {
		app.Use(basicauth.New(basicauth.Config{
			Users: map[string]string{
				username: password,
			},
			Realm: "journalctl proxy",
		}))
	}

	assetsFS, err := fs.Sub(assets, "assets")
	if err != nil {
		log.Fatal(err)
	}
	app.Use(filesystem.New(filesystem.Config{
		Root:  http.FS(assetsFS),
		Index: "index.html",
	}))

	app.Get("/list-services", func(c *fiber.Ctx) error {
		args := []string{"list-units", "--type=service", "--plain", "--no-pager"}
		if user {
			args = append(args, "--user")
		}
		out, err := exec.Command("systemctl", args...).Output()

		if err != nil {
			fmt.Printf("%s", err)
		}

		outStr := string(out[:])

		if docker {
			dockerOut, err := exec.Command("docker", "ps", "-a", "--no-trunc", "--format", `"{{.ID}}":"{{.Names}}",`).Output()
			if err != nil {
				fmt.Printf("%s", err)
			} else {
				dockerLookup := map[string]string{}
				json.Unmarshal([]byte(`{`+string(dockerOut)+`"":""}`), &dockerLookup)

				for id, name := range dockerLookup {
					if id != "" {
						outStr = id + ".docker human-name=" + name + ".docker\n" + outStr
					}
				}
			}
		}

		return c.SendString(outStr)
	})

	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		var messageType int = 1
		var message []byte
		var err error

		services := []string{}
		json.Unmarshal([]byte(c.Query("services")), &services)

		args := []string{"-b"}
		if user {
			args = append(args, "--user")
		}
		for _, service := range services {
			if docker && strings.HasSuffix(service, ".docker") {
				args = append(args, "CONTAINER_ID_FULL="+strings.TrimSuffix(service, ".docker"), "+")
			} else if user {
				args = append(args, "_SYSTEMD_USER_UNIT="+service, "+")
			} else {
				args = append(args, "_SYSTEMD_UNIT="+service, "+")
			}
		}
		if args[len(args)-1] == "+" {
			args = args[:len(args)-1]
		}
		args = append(args, "--all", "-f", "-n", "100", "-o", "json")


		ctx, cancel := context.WithCancel(context.Background())
		c.SetCloseHandler(func(code int, text string) error {
			cancel()
			return nil
		})
		cmd := exec.CommandContext(ctx, "journalctl", args...)
		stdout, _ := cmd.StdoutPipe()
		cmd.Start()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			message = []byte(scanner.Text())
			if err = c.WriteMessage(messageType, message); err != nil {
				c.Close()
				cancel()
			}
		}

		cmd.Wait()
	}, websocket.Config{
		WriteBufferSize: 8192,
	}))

	log.Fatal(app.Listen(fmt.Sprintf(":%d", port)))
}

//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/magefile/mage/mg"
)

const (
	DB_URL      = "postgres://postgres:password@localhost:5432/chat_app?sslmode=disable"
	DOCKER_FILE = "../docker-compose.yml"
	BINARY_NAME = "../bin/chat-server"
	MAIN_PATH   = "../cmd/server/main.go"
)

func DockerUp() error {
	fmt.Println("🚀 Starting Postgres container...")
	return runCmd("docker-compose", "-f", DOCKER_FILE, "up", "-d")
}

func DockerDown() error {
	fmt.Println("🛑 Stopping Postgres container...")
	return runCmd("docker-compose", "-f", DOCKER_FILE, "down")
}
func DockerStop() error {
	fmt.Println("⏸️  Stopping Postgres container (retaining instance)...")
	return runCmd("docker-compose", "-f", DOCKER_FILE, "stop")
}

func DockerStart() error {
	fmt.Println("▶️  Starting existing Postgres container...")
	return runCmd("docker-compose", "-f", DOCKER_FILE, "start")
}

func MigrateUp() error {
	fmt.Println("⬆️  Running migrations up...")
	return runCmd("migrate", "-path", "../migrations", "-database", DB_URL, "up")
}

func MigrateDown() error {
	fmt.Println("⬇️  Rolling back 1 migration...")
	return runCmd("migrate", "-path", "../migrations", "-database", DB_URL, "down", "1")
}

func Build() error {
	fmt.Println("🔨 Building server binary...")
	return runCmd("go", "build", "-o", BINARY_NAME, MAIN_PATH)
}

func Clean() {
	fmt.Println("🧹 Cleaning up...")
	os.Remove(BINARY_NAME)
	mg.Deps(DockerDown)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

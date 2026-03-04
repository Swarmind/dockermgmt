package main

import (
	"context"
	containermgmt "dockermgmt/pkg/containerMgmt"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func main() {
	// Контекст с таймаутом для долгих операций (например, pull образа)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Создаём клиент Docker (будет использоваться во многих функциях)
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Fatalf("Cannot create Docker client: %v", err)
	}
	defer cli.Close()

	// Проверяем, что Docker доступен (ping)
	if _, err := cli.Ping(ctx); err != nil {
		log.Fatalf("Docker is not responding: %v", err)
	}
	fmt.Println("Docker daemon is available.")

	// --- Шаг 1: Подготовка тестового шаблона (Template) ---
	// Для теста используем alpine:latest, который будет спать 1 час,
	// чтобы можно было выполнять различные операции.
	// OnStartScript создаст файл /hello.txt и запустит sleep.
	onStartScript := `#!/bin/sh
echo "Container started at $(date)" > /hello.txt
sleep 3600
`
	template := containermgmt.Template{
		ID:                   1,
		TemplateName:         "test-alpine",
		TemplateDescription:  "Test template with alpine",
		OnStartScript:        onStartScript,
		ExtraFilters:         []string{"test=yes", "source=example"},
		VRAMRequiredGB:       0, // GPU не нужен
		MaxPricePerHourCents: 0,
		DockerServerName:     "", // public hub
		DockerUsername:       "",
		DockerPassword:       "",
		IsPrivate:            false,
		ImagePath:            "alpine:latest",
		DockerOptions:        "",
		Ports:                []string{"8080:80"}, // проброс порта, но alpine не слушает 80, просто для демонстрации
		EnvironmentVariables: []string{"TEST_ENV=hello"},
		Readme:               "",
	}

	// --- Шаг 2: Запуск контейнера ---
	fmt.Println("\n--- Launching container ---")
	if err := containermgmt.LaunchContainer(ctx, template); err != nil {
		log.Fatalf("Failed to launch container: %v", err)
	}

	// Находим только что запущенный контейнер по имени (TemplateName)
	containers, err := containermgmt.ListContainers(ctx, cli, true, map[string]string{
		"label": "template-name=" + template.TemplateName,
	})
	if len(containers) == 0 {
		log.Fatalf("Container %s not found", template.TemplateName)
	}
	containerID := containers[0].ID
	fmt.Printf("Container ID: %s\n", containerID)

	// --- Шаг 3: Получение статуса (inspect) ---
	fmt.Println("\n--- Container status (inspect) ---")
	status, err := containermgmt.GetContainerStatus(ctx, cli, containerID)
	if err != nil {
		log.Printf("Failed to get container status: %v", err)
	} else {
		fmt.Printf("Container name: %s\n", status.Name)
		fmt.Printf("State: %s\n", status.State.Status)
		fmt.Printf("Image: %s\n", status.Image)
		fmt.Printf("Labels: %v\n", status.Config.Labels)
	}

	// --- Шаг 4: Логи контейнера (первые несколько строк) ---
	fmt.Println("\n--- Container logs (tail 10) ---")
	logsReader, err := containermgmt.GetContainerLogs(ctx, cli, containerID, false, "10")
	if err != nil {
		log.Printf("Failed to get logs: %v", err)
	} else {
		defer logsReader.Close()
		// docker демон возвращает логи в multiplexed формате, используем io.Copy для простоты
		// но можно и демодультиплексировать, для теста сойдет
		io.Copy(os.Stdout, logsReader)
		fmt.Println()
	}

	// --- Шаг 5: Статистика в текстовом виде ---
	fmt.Println("\n--- Container stats (plain text) ---")
	statsText, err := containermgmt.GetContainerStatsPlainText(ctx, cli, containerID)
	if err != nil {
		log.Printf("Failed to get stats: %v", err)
	} else {
		fmt.Print(statsText)
	}

	// --- Шаг 6: Обновление ресурсов (пример, ничего не меняем) ---
	fmt.Println("\n--- Update resources (no changes) ---")
	if err := containermgmt.UpdateContainerResources(ctx, cli, containerID, container.Resources{}); err != nil {
		log.Printf("Failed to update resources: %v", err)
	} else {
		fmt.Println("Resources updated (no changes).")
	}

	// --- Шаг 7: Пауза и возобновление ---
	fmt.Println("\n--- Pause container ---")
	if err := containermgmt.PauseContainer(ctx, cli, containerID); err != nil {
		log.Printf("Failed to pause container: %v", err)
	} else {
		fmt.Println("Container paused.")
		time.Sleep(5 * time.Second)

		fmt.Println("--- Unpause container ---")
		if err := containermgmt.UnpauseContainer(ctx, cli, containerID); err != nil {
			log.Printf("Failed to unpause container: %v", err)
		} else {
			fmt.Println("Container unpaused.")
		}
	}

	// --- Шаг 8: Ожидание изменения состояния (Wait) в отдельной горутине ---
	fmt.Println("\n--- Waiting for container stop (async) ---")
	waitC, errC := containermgmt.WaitContainer(ctx, cli, containerID, container.WaitConditionNotRunning)

	// Дадим контейнеру поработать ещё немного, потом остановим
	time.Sleep(10 * time.Second)

	// Останавливаем контейнер (не удаляем)
	fmt.Println("--- Stopping container (graceful, timeout 10s) ---")
	timeout := 10 * time.Second
	if err := containermgmt.StopContainer(ctx, cli, containerID, &timeout); err != nil {
		log.Printf("Failed to stop container: %v", err)
	} else {
		fmt.Println("Container stopped.")
	}

	// Ждём результата ожидания
	select {
	case res := <-waitC:
		if res.Error != nil {
			log.Printf("Wait error: %v", res.Error.Message)
		} else {
			fmt.Printf("Wait condition met: exit code %d\n", res.StatusCode)
		}
	case err := <-errC:
		log.Printf("Wait channel error: %v", err)
	case <-time.After(30 * time.Second):
		log.Println("Wait timeout")
	}

	// --- Шаг 9: Перезапуск остановленного контейнера ---
	fmt.Println("\n--- Restarting container ---")
	if err := containermgmt.RestartContainer(ctx, cli, containerID, &timeout); err != nil {
		log.Printf("Failed to restart container: %v", err)
	} else {
		fmt.Println("Container restarted.")
		// Подождём немного, чтобы убедиться, что работает
		time.Sleep(5 * time.Second)
	}

	// --- Шаг 10: Запуск остановленного (сейчас он запущен после restart) - для демонстрации StartContainer ---
	fmt.Println("\n--- Stopping again and starting ---")
	if err := containermgmt.StopContainer(ctx, cli, containerID, &timeout); err != nil {
		log.Printf("Failed to stop container: %v", err)
	} else {
		fmt.Println("Container stopped.")
		time.Sleep(2 * time.Second)
		if err := containermgmt.StartContainer(ctx, cli, containerID); err != nil {
			log.Printf("Failed to start container: %v", err)
		} else {
			fmt.Println("Container started.")
		}
	}

	// --- Шаг 11: Attach (только демонстрация создания подключения, интерактив не используется) ---
	fmt.Println("\n--- Attach to container (non-interactive) ---")
	attachOpts := container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	}
	hijackedResp, err := containermgmt.AttachContainer(ctx, cli, containerID, attachOpts)
	if err != nil {
		log.Printf("Failed to attach: %v", err)
	} else {
		fmt.Println("Attach successful (hijacked connection). Closing immediately.")
		hijackedResp.Close()
	}

	// --- Шаг 12: Список всех управляемых контейнеров ---
	fmt.Println("\n--- List all managed containers ---")
	allContainers, err := containermgmt.ListContainers(ctx, cli, true, nil)
	if err != nil {
		log.Printf("Failed to list containers: %v", err)
	} else {
		fmt.Printf("Found %d managed container(s):\n", len(allContainers))
		for _, c := range allContainers {
			fmt.Printf("  - %s (%s) status: %s\n", c.ID[:12], c.Names, c.Status)
		}
	}

	// --- Шаг 13: Удаление контейнера (с force и удалением томов) ---
	fmt.Println("\n--- Removing container ---")
	if err := containermgmt.RemoveContainer(ctx, cli, containerID, true, true); err != nil {
		log.Printf("Failed to remove container: %v", err)
	} else {
		fmt.Printf("Container %s removed.\n", containerID[:12])
	}

	fmt.Println("\nTest completed.")
}

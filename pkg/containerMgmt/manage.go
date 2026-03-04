package containermgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// ensureManagedContainer проверяет, что контейнер помечен как управляемый нашим приложением
func ensureManagedContainer(ctx context.Context, cli *client.Client, containerID string) error {
	insp, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}
	if val, ok := insp.Config.Labels["managed-by"]; !ok || val != "template-manager" {
		return fmt.Errorf("container %s is not managed by this application", containerID)
	}
	return nil
}

// StopContainer останавливает контейнер (с таймаутом)
func StopContainer(ctx context.Context, cli *client.Client, containerID string, timeout *time.Duration) error {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return err
	}
	var stopTimeout *int
	if timeout != nil {
		sec := int(timeout.Seconds())
		stopTimeout = &sec
	}
	if err := cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: stopTimeout}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// StartContainer запускает остановленный контейнер
func StartContainer(ctx context.Context, cli *client.Client, containerID string) error {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return err
	}
	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// RestartContainer перезапускает контейнер
func RestartContainer(ctx context.Context, cli *client.Client, containerID string, timeout *time.Duration) error {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return err
	}
	var restartTimeout *int
	if timeout != nil {
		sec := int(timeout.Seconds())
		restartTimeout = &sec
	}
	if err := cli.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: restartTimeout}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}
	return nil
}

// RemoveContainer удаляет контейнер (с опциями force и удаление томов)
func RemoveContainer(ctx context.Context, cli *client.Client, containerID string, force, removeVolumes bool) error {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return err
	}

	// Получаем информацию о контейнере, чтобы найти путь к скрипту
	containerInfo, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	// Удаляем контейнер
	removeOpts := container.RemoveOptions{
		Force:         force,
		RemoveVolumes: removeVolumes,
	}
	if err := cli.ContainerRemove(ctx, containerID, removeOpts); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	// После успешного удаления контейнера удаляем связанный скрипт
	if scriptPath, ok := containerInfo.Config.Labels["onstart-script-path"]; ok && scriptPath != "" {
		if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
			// Логируем ошибку, но не возвращаем её, так как контейнер уже удалён
			log.Printf("Warning: failed to remove script file %s: %v", scriptPath, err)
		}
	}

	return nil
}

// GetContainerStatus возвращает детальную информацию о контейнере (аналог docker inspect)
func GetContainerStatus(ctx context.Context, cli *client.Client, containerID string) (types.ContainerJSON, error) {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return types.ContainerJSON{}, err
	}
	insp, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return types.ContainerJSON{}, fmt.Errorf("failed to inspect container: %w", err)
	}
	return insp, nil
}

// GetContainerLogs возвращает поток логов контейнера
// follow: если true, читает логи в реальном времени; tail: количество строк от конца (например "100" или "all")
func GetContainerLogs(ctx context.Context, cli *client.Client, containerID string, follow bool, tail string) (io.ReadCloser, error) {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return nil, err
	}
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
	}
	logs, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}
	return logs, nil
}

// ListContainers возвращает список контейнеров, управляемых приложением.
// all: если true, включает остановленные; filters: дополнительные фильтры (например, map[string]string{"status":"running"})
func ListContainers(ctx context.Context, cli *client.Client, all bool, extraFilters map[string]string) ([]types.Container, error) {
	// Базовый фильтр по нашей метке
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "managed-by=template-manager")

	// Добавляем пользовательские фильтры
	for key, value := range extraFilters {
		filterArgs.Add(key, value)
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     all,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	return containers, nil
}

// UpdateContainerResources изменяет ресурсные ограничения контейнера на лету (CPU, память и т.д.)
func UpdateContainerResources(ctx context.Context, cli *client.Client, containerID string, resources container.Resources) error {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return err
	}
	if _, err := cli.ContainerUpdate(ctx, containerID, container.UpdateConfig{
		Resources: resources,
	}); err != nil {
		return fmt.Errorf("failed to update container resources: %w", err)
	}
	return nil
}

// PauseContainer приостанавливает все процессы в контейнере (используя cgroups freezer)
func PauseContainer(ctx context.Context, cli *client.Client, containerID string) error {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return err
	}
	if err := cli.ContainerPause(ctx, containerID); err != nil {
		return fmt.Errorf("failed to pause container: %w", err)
	}
	return nil
}

// UnpauseContainer возобновляет выполнение приостановленного контейнера
func UnpauseContainer(ctx context.Context, cli *client.Client, containerID string) error {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return err
	}
	if err := cli.ContainerUnpause(ctx, containerID); err != nil {
		return fmt.Errorf("failed to unpause container: %w", err)
	}
	return nil
}

// WaitContainer ожидает изменения состояния контейнера (остановка, удаление и т.д.)
// Возвращает канал с результатом ожидания и канал ошибок.
func WaitContainer(ctx context.Context, cli *client.Client, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	// Здесь не проверяем метку, потому что это асинхронная операция и контейнер может быть уже удалён.
	// Но можно проверить перед вызовом.
	return cli.ContainerWait(ctx, containerID, condition)
}

// Возвращает container.StatsResponseReader
// Это обёртка над:
// Body io.ReadCloser
// методом Close()
// Если stream = false → придёт один JSON
// Если stream = true → придёт бесконечный поток JSON объектов
// И важное: ты обязан закрыть reader, иначе словишь утечки.
func ContainerStats(
	ctx context.Context,
	cli *client.Client,
	containerID string,
	stream bool,
) (container.StatsResponseReader, error) {

	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return container.StatsResponseReader{}, err
	}

	stats, err := cli.ContainerStats(ctx, containerID, stream)
	if err != nil {
		return container.StatsResponseReader{}, fmt.Errorf("failed to get container stats: %w", err)
	}

	return stats, nil
}

func GetContainerStatsPlainText(
	ctx context.Context,
	cli *client.Client,
	containerID string,
) (string, error) {

	statsReader, err := ContainerStats(ctx, cli, containerID, false)
	if err != nil {
		return "", err
	}
	defer statsReader.Body.Close()

	var stats container.StatsResponse
	if err := json.NewDecoder(statsReader.Body).Decode(&stats); err != nil {
		return "", fmt.Errorf("failed to decode stats: %w", err)
	}

	// ---- CPU ----
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)

	var cpuPercent float64
	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / systemDelta) *
			float64(len(stats.CPUStats.CPUUsage.PercpuUsage)) * 100.0
	}

	// ---- Memory ----
	memUsage := float64(stats.MemoryStats.Usage)
	memLimit := float64(stats.MemoryStats.Limit)

	memPercent := 0.0
	if memLimit > 0 {
		memPercent = (memUsage / memLimit) * 100.0
	}

	plaintext := fmt.Sprintf(
		"CPU: %.2f%%\nMemory: %.2f MB / %.2f MB (%.2f%%)\n",
		cpuPercent,
		memUsage/1024/1024,
		memLimit/1024/1024,
		memPercent,
	)

	return plaintext, nil
}

// AttachContainer подключается к контейнеру для интерактивного взаимодействия (например, для входа в shell)
// Опции можно настроить под конкретные нужды (stdin, stdout, stderr, stream, detach keys и т.д.)
func AttachContainer(ctx context.Context, cli *client.Client, containerID string, options container.AttachOptions) (types.HijackedResponse, error) {
	if err := ensureManagedContainer(ctx, cli, containerID); err != nil {
		return types.HijackedResponse{}, err
	}
	resp, err := cli.ContainerAttach(ctx, containerID, options)
	if err != nil {
		return types.HijackedResponse{}, fmt.Errorf("failed to attach to container: %w", err)
	}
	return resp, nil
}

package containermgmt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// LaunchContainer создаёт и запускает контейнер на основе Template
func LaunchContainer(ctx context.Context, t Template) error {
	// 1. Создаём клиент Docker
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer cli.Close()

	// 2. Формируем полное имя образа (с учётом DockerServerName)
	imageName := t.ImagePath
	if t.DockerServerName != "" && !strings.Contains(t.ImagePath, "/") {
		// Если сервер указан и путь не содержит слеша, добавляем префикс
		imageName = t.DockerServerName + "/" + t.ImagePath
	}

	// 3. Аутентификация для pull (если есть логин/пароль)
	var authConfig registry.AuthConfig
	if t.DockerUsername != "" && t.DockerPassword != "" {
		authConfig = registry.AuthConfig{
			Username:      t.DockerUsername,
			Password:      t.DockerPassword,
			ServerAddress: t.DockerServerName,
		}
	}
	authStr := ""
	if authConfig.Username != "" {
		encodedJSON, err := json.Marshal(authConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal auth config: %w", err)
		}
		authStr = base64.URLEncoding.EncodeToString(encodedJSON)
	}

	// 4. Pull образа
	out, err := cli.ImagePull(ctx, imageName, image.PullOptions{
		RegistryAuth: authStr,
	})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer out.Close()
	// Печатаем лог pull (можно заменить на логирование)
	io.Copy(os.Stdout, out)

	// 5. Подготовка меток (labels) из ExtraFilters
	labels := make(map[string]string)
	for _, filter := range t.ExtraFilters {
		parts := strings.SplitN(filter, "=", 2)
		if len(parts) == 2 {
			labels[parts[0]] = parts[1]
		} else {
			// если нет '=', используем ключ с пустым значением
			labels[filter] = ""
		}
	}

	labels["template-name"] = t.TemplateName
	labels["managed-by"] = "template-manager"

	// 6. Переменные окружения
	env := t.EnvironmentVariables

	if t.DockerOptions != "" {
		optEnv, optLabels, err := parseDockerOptions(t.DockerOptions)
		if err != nil {
			return fmt.Errorf("failed to parse DockerOptions: %w", err)
		}
		env = append(env, optEnv...)
		for k, v := range optLabels {
			labels[k] = v
		}
	}
	// 7. Обработка OnStartScript: создаём постоянный файл
	var cmd []string
	var binds []string
	var scriptHostPath string

	if t.OnStartScript != "" {
		// Создаём директорию для скриптов, если её нет
		scriptsDir := filepath.Join(os.TempDir(), ".template-manager", "scripts")
		if err := os.MkdirAll(scriptsDir, 0755); err != nil {
			return fmt.Errorf("failed to create scripts directory: %w", err)
		}

		// Создаём файл скрипта с именем контейнера
		// Очищаем имя контейнера от недопустимых символов для имени файла
		safeContainerName := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, t.TemplateName)

		scriptHostPath = filepath.Join(scriptsDir, safeContainerName+".sh")

		// Записываем скрипт в файл
		if err := os.WriteFile(scriptHostPath, []byte(t.OnStartScript), 0755); err != nil {
			return fmt.Errorf("failed to write script file: %w", err)
		}

		// Добавляем метку с путём к скрипту для последующего удаления
		labels["onstart-script-path"] = scriptHostPath

		// Bind mount: файл хоста -> /onstart.sh внутри контейнера (read-only)
		binds = append(binds, fmt.Sprintf("%s:/onstart.sh:ro", scriptHostPath))

		// Команда контейнера — выполнить скрипт
		cmd = []string{"/onstart.sh"}
		// Если нужно, чтобы после скрипта запускался оригинальный процесс,
		// скрипт должен сам вызвать exec (например, добавив в конце exec "$@").
	}

	// 8. Проброс портов
	portBindings := nat.PortMap{}
	for _, p := range t.Ports {
		// Ожидаем формат: "hostPort:containerPort" или "containerPort" (тогда hostPort будет случайным)
		parts := strings.Split(p, ":")
		var hostPort, containerPort string
		switch len(parts) {
		case 1:
			containerPort = parts[0]
			hostPort = "" // Docker назначит случайный порт
		case 2:
			hostPort = parts[0]
			containerPort = parts[1]
		default:
			// Некорректный формат — пропускаем
			continue
		}

		// Предполагаем протокол TCP (можно расширить, если нужно)
		port, err := nat.NewPort("tcp", containerPort)
		if err != nil {
			continue
		}
		portBindings[port] = []nat.PortBinding{
			{
				HostIP:   "0.0.0.0",
				HostPort: hostPort,
			},
		}
	}

	// 9. Настройки HostConfig
	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		StorageOpt:   make(map[string]string),
		Binds:        binds,
		Resources:    container.Resources{},
	}

	// 10. Основная конфигурация контейнера
	config := &container.Config{
		Image:  imageName,
		Env:    env,
		Labels: labels,
		Cmd:    cmd,
		// Если нужно изменить Entrypoint, можно задать здесь
		// Entrypoint: []string{...}
	}

	// 11. Создание контейнера
	// Имя контейнера берётся из TemplateName (если пустое, Docker сгенерирует сам)
	resp, err := cli.ContainerCreate(ctx, config, hostConfig, nil, nil, t.TemplateName)
	if err != nil {
		// В случае ошибки создания контейнера удаляем созданный скрипт
		if scriptHostPath != "" {
			os.Remove(scriptHostPath)
		}
		return fmt.Errorf("failed to create container: %w", err)
	}

	// 12. Запуск контейнера
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// В случае ошибки запуска удаляем скрипт
		if scriptHostPath != "" {
			os.Remove(scriptHostPath)
		}
		return fmt.Errorf("failed to start container: %w", err)
	}

	fmt.Printf("Container %s started with ID %s\n", t.TemplateName, resp.ID)
	return nil
}

func setOrAppendEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func parseDockerOptions(options string) (envVars []string, extraLabels map[string]string, err error) {
	extraLabels = make(map[string]string)
	if options == "" {
		return
	}

	// Разбиваем строку по пробелам (упрощённо)
	parts := strings.Fields(options)
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		switch part {
		case "-e", "--env":
			if i+1 >= len(parts) {
				err = fmt.Errorf("missing argument for %s", part)
				return
			}
			envVars = append(envVars, parts[i+1])
			i++ // пропускаем аргумент
		case "-l", "--label":
			if i+1 >= len(parts) {
				err = fmt.Errorf("missing argument for %s", part)
				return
			}
			labelPair := parts[i+1]
			kv := strings.SplitN(labelPair, "=", 2)
			if len(kv) == 2 {
				extraLabels[kv[0]] = kv[1]
			} else {
				extraLabels[kv[0]] = "" // если нет значения
			}
			i++
		default:
			// Пропускаем неизвестные опции (или можно вернуть ошибку)
			// Например: --shm-size, --ulimit и т.п. пока не поддерживаются
		}
	}
	return
}

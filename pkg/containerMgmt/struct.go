package containermgmt

type Template struct {
	ID                   int      `json:"id"`
	TemplateName         string   `json:"template_name"`
	TemplateDescription  string   `json:"template_description"`
	OnStartScript        string   `json:"on_start_script"`
	ExtraFilters         []string `json:"extra_filters"`
	VRAMRequiredGB       int      `json:"vram_required_gb"`
	MaxPricePerHourCents int      `json:"max_price_per_hour_cents"`
	DockerServerName     string   `json:"docker_server_name"`
	DockerUsername       string   `json:"docker_username"`
	DockerPassword       string   `json:"docker_password"`
	//DiskSpaceMB          int      `json:"disk_space_mb"`
	IsPrivate            bool     `json:"is_private"`
	ImagePath            string   `json:"image_path"`
	DockerOptions        string   `json:"docker_options"`
	Ports                []string `json:"ports"`
	EnvironmentVariables []string `json:"environment_variables"`
	Readme               string   `json:"readme"`
}

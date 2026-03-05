package containermgmt

type Template struct {
	ID           int    `json:"id"`
	TemplateName string `json:"template_name"`
	//TemplateDescription  string   `json:"template_description"` <-For frontend only
	OnStartScript  string   `json:"on_start_script"`
	ExtraFilters   []string `json:"extra_filters"`
	VRAMRequiredGB int      `json:"vram_required_gb"`
	//MaxPricePerHourCents int      `json:"max_price_per_hour_cents"` <-For frontend only
	DockerServerName string `json:"docker_server_name"`
	DockerUsername   string `json:"docker_username"`
	DockerPassword   string `json:"docker_password"`
	//DiskSpaceMB          int      `json:"disk_space_mb"`  <-For resizer only
	//IsPrivate            bool     `json:"is_private"` 	<-For frontend only
	ImagePath            string   `json:"image_path"`
	DockerOptions        string   `json:"docker_options"`
	Ports                []string `json:"ports"`
	EnvironmentVariables []string `json:"environment_variables"`
	//Readme               string   `json:"readme"`   <-For frontend only
}

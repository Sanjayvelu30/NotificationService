package repository

type TemplateRepo interface {
	Save(name string, body string, userID string) error
	Get(name string, userID string) (string, error)
	GetAll(userID string) (map[string]string, error)
}

package errors

type ErrorCode string

type LocalizedErrorDetail struct{}

func NewLocalizedErrorDetail(args ...any) LocalizedErrorDetail {
	return LocalizedErrorDetail{}
}

func LocalizedErrorDetailFromError(err error) LocalizedErrorDetail {
	return LocalizedErrorDetail{}
}

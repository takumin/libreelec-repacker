package metadata

var (
	appName    string = "libreelec-repacker"
	appDesc    string = "Declaratively customize and repack official LibreELEC disk images without rebuilding from source."
	authorName string = "Takumi Takahashi"
)

func AppName() string {
	return appName
}

func AppDesc() string {
	return appDesc
}

func AuthorName() string {
	return authorName
}

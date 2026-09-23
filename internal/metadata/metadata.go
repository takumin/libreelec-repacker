package metadata

var (
	appName    string = "boilerplate-golang-cli"      // ###BOILERPLATE_APP_NAME###
	appDesc    string = "Boilerplate Golang CLI Tool" // ###BOILERPLATE_APP_DESC###
	authorName string = "Takumi Takahashi"            // ###BOILERPLATE_AUTHOR_NAME###
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

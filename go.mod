module github.com/kserve/modelmesh-runtime-adapter

go 1.19

require (
	cloud.google.com/go/storage v1.28.1
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v0.13.2
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v0.3.0
	github.com/IBM/ibm-cos-sdk-go v1.9.1
	github.com/cyphar/filepath-securejoin v0.2.4
	github.com/go-logr/logr v1.2.3
	github.com/go-logr/zapr v1.2.3
	github.com/golang/mock v1.6.0
	github.com/joho/godotenv v1.4.0
	github.com/stretchr/testify v1.8.4
	go.uber.org/zap v1.24.0
	golang.org/x/sync v0.1.0
	google.golang.org/api v0.114.0
	google.golang.org/grpc v1.56.3
	google.golang.org/protobuf v1.30.0
	// controller-runtime dependency is only used for logging
	sigs.k8s.io/controller-runtime v0.14.6
)

require (
	cloud.google.com/go v0.110.0 // indirect
	cloud.google.com/go/compute v1.19.1 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	cloud.google.com/go/iam v0.13.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v0.21.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v0.9.1 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v0.4.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt v3.2.1+incompatible // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.2.3 // indirect
	github.com/googleapis/gax-go/v2 v2.7.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pkg/browser v0.0.0-20210115035449-ce105d075bb4 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.11.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/oauth2 v0.7.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apimachinery v0.26.1 // indirect
	k8s.io/klog/v2 v2.90.1 // indirect
	k8s.io/utils v0.0.0-20230209194617-a36077c30491 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
)

replace (
	// Update to avoid CVE-2022-27191, CVE-2021-43565, CVE-2020-29652, CVE-2023-48795
	golang.org/x/crypto => golang.org/x/crypto v0.17.0
	// Update to avoid CVE-2023-3978, CVE-2023-39325, CVE-2023-44487
	golang.org/x/net => golang.org/x/net v0.17.0
	// remove when upgrade to controller-runtime 0.15.x or apimachinery to 0.27.x
	// Fixes github.com/elazarl/goproxy Denial of Service (DoS)
	// This dependency was removed from apimachinery 0.27.0
	// Even the controller-runtime being used only for logging, the version 0.15.0 brings
	// apimachinery 0.27.0 that brings a lot more of indirect dependencies that we don't want to pull
	k8s.io/apimachinery => k8s.io/apimachinery v0.27.0
)

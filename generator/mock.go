package generator

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cv21/gen/pkg"
	. "github.com/dave/jennifer/jen"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/iancoleman/strcase"
	"github.com/vetcher/go-astra/types"
)

const (
	// It is useful for comments in generated files.
	pluginRepoURL = "github.com/cv21/gen-generator-mock"
	pluginVersion = "1.0.0"

	mockPackage = "github.com/stretchr/testify/mock"
)

type (
	// It is custom parameters for mock generator.
	generatorParams struct {
		// This is name of interface to mock.
		// Example: SomeService
		InterfaceName string `json:"interface_name"`

		// It is output path template.
		// It applies %s literal which holds interface name in snack_case.
		// Example: ./generated/%s_mock_gen.go
		OutPathTemplate string `json:"out_path_template"`

		// It is package of source file.
		// Example: github.com/cv21/gen-generator-mock/examples/stringsvc
		SourcePackagePath string `json:"source_package_path"`

		// It is target package path.
		// Example: github.com/cv21/gen-generator-mock/examples/stringsvc/bla
		TargetPackagePath string `json:"target_package_path"`

		// It is a template for custom struct naming.
		// It applies %s literal which holds interface name.
		// Example: MyPrettyMockOf%s
		MockStructNameTemplate string `json:"mock_struct_name_template"`
	}

	mockGenerator struct {
		logger hclog.Logger
	}
)

// Implements Generator interface.
// Generates mock files.
func (m *mockGenerator) Generate(p *pkg.GenerateParams) (*pkg.GenerateResult, error) {
	params := &generatorParams{}
	err := json.Unmarshal(p.Params, params)
	if err != nil {
		return nil, err
	}

	iface := pkg.FindInterface(p.File, params.InterfaceName)

	f := NewFilePath(params.TargetPackagePath)

	mockStructName := m.buildMockStructName(params.MockStructNameTemplate, iface.Name)

	f.Add(m.generateType(mockStructName, iface.Name)).Line()

	for _, method := range iface.Methods {
		f.Add(m.generateMethod(params, iface.Name, mockStructName, method)).Line()
	}

	return &pkg.GenerateResult{
		Files: []pkg.GenerateResultFile{
			{
				Path:    fmt.Sprintf(params.OutPathTemplate, strcase.ToSnake(iface.Name)),
				Content: []byte(fmt.Sprintf("%#v", pkg.AddDefaultPackageComment(f, pluginRepoURL, pluginVersion))),
			},
		},
	}, nil
}

// Generates type declaration. For example:
//
// // StringServiceMock is an autogenerated mock type for the StringService interface.
// type StringServiceMock struct {
// 		mock.Mock
// }
func (m *mockGenerator) generateType(mockStructName, interfaceName string) *Statement {
	return Commentf("%s is an autogenerated mock type for the %s interface.", mockStructName, interfaceName).Line().
		Type().Id(mockStructName).Struct(
		Qual(mockPackage, "Mock"),
	)
}

// Generates method declaration. For example:
//
// // Concat provides a mock function for method Concat of interface StringService.
// func (_m *StringServiceMock) Concat(a string, b string) string {
//		ret := _m.Called(a, b)
//
//		var r0 string
//		if rf, ok := ret.Get(0).(func(string, string) string); ok {
//			r0 = rf(a, b)
//		} else {
//			r0 = ret.Get(0).(string)
//		}
//
//		return r0
// }
//
func (m *mockGenerator) generateMethod(params *generatorParams, interfaceName, mockStructName string, method *types.Function) *Statement {
	return Commentf("%s provides a mock function for method %s of interface %s.", method.Name, method.Name, interfaceName).Line().
		Func().Params(Id("_m").Id(fmt.Sprintf("*%s", mockStructName))).Id(method.Name).ParamsFunc(func(g *Group) {
		for _, a := range method.Args {
			g.Id(a.Name).Add(typeQual(params, a.Type))
		}
	}).ParamsFunc(func(g *Group) {
		for _, r := range method.Results {
			g.Id(r.Name).Add(typeQual(params, r.Type))
		}
	}).BlockFunc(func(g *Group) {
		g.Id("ret").Op(":=").Id("_m.Called").ParamsFunc(func(g *Group) {
			for _, a := range method.Args {
				g.Id(a.Name)
			}
		}).Line()

		var retNames []string
		for i, r := range method.Results {
			currentRetName := fmt.Sprintf("r%d", i)
			retNames = append(retNames, currentRetName)

			g.Var().Id(currentRetName).Add(typeQual(params, r.Type))
			g.If(List(Id("rf"), Id("ok").Op(":=").Id("ret.Get").Call(Lit(i))).Assert(Func().ParamsFunc(func(g *Group) {
				for _, a := range method.Args {
					g.Add(typeQual(params, a.Type))
				}
			}).Add(typeQual(params, r.Type))), Id("ok")).BlockFunc(func(g *Group) {
				g.Id(currentRetName).Op("=").Id("rf").ParamsFunc(func(g *Group) {
					for _, a := range method.Args {
						g.Id(a.Name)
					}
				})
			}).Else().BlockFunc(func(g *Group) {
				if pkg.IsErrorType(r.Type) {
					// 		r0 = ret.Error(0)
					g.Id(currentRetName).Op("=").Id("ret.Error").Params(Lit(i))
				} else {
					if pkg.IsNillableType(r.Type) {
						// 		if ret.Get(0) != nil {
						//			r0 = ret.Get(0).(*bla.Bla)
						//		}
						g.If(Id("ret.Get").Params(Lit(i)).Op("!=").Nil()).BlockFunc(func(g *Group) {
							g.Add(Id(currentRetName).Op("=").Id("ret.Get").Params(Lit(i)).Assert(typeQual(params, r.Type)))
						})
					} else {
						// 		r0 = ret.Get(0).(*bla.Bla)
						g.Id(currentRetName).Op("=").Id("ret.Get").Params(Lit(i)).Assert(typeQual(params, r.Type))
					}

				}
			}).Line()
		}

		g.ReturnFunc(func(g *Group) {
			for _, retName := range retNames {
				g.Add(Id(retName))
			}
		})
	})
}

// Returns a mock structure name by given interfaceName.
func (m *mockGenerator) buildMockStructName(template string, interfaceName string) string {
	if template == "" {
		template = "%sMock"
	}
	return fmt.Sprintf(template, interfaceName)
}

// It is a convenient func for calling pkg.TypeQual.
func typeQual(params *generatorParams, t types.Type) *Statement {
	return pkg.TypeQual(params.SourcePackagePath, params.TargetPackagePath, t)
}

// Allocates and returns new structure of mockGenerator.
func NewGenerator() pkg.Generator {
	return &mockGenerator{
		logger: hclog.New(&hclog.LoggerOptions{
			Level:      hclog.Trace,
			Output:     os.Stderr,
			JSONFormat: true,
		}),
	}
}

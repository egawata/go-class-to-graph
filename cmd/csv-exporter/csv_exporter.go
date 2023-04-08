package main

import (
	"context"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"sync"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

var (
	filename string
)

type objType int

func (t objType) String() string {
	switch t {
	case typeStruct:
		return "struct"
	case typeInterface:
		return "interface"
	default:
		return "unknown"
	}
}

const (
	typeStruct objType = iota
	typeInterface
)

type AllStructs map[string]map[string]*Struct

type Struct struct {
	sync.Mutex
	Type    objType
	Package string
	Name    string
	Methods []*Method
}

func (s *Struct) AddMethod(m *Method) {
	s.Lock()
	defer s.Unlock()

	if s.Methods == nil {
		s.Methods = []*Method{}
	}
	s.Methods = append(s.Methods, m)
}

func main() {
	flag.StringVar(&filename, "f", "", "filename to parse")
	flag.Parse()
	if filename == "" {
		flag.Usage()
		os.Exit(1)
	}

	ss := parseFile(filename)
	renderFile(ss)
}

func parseFile(filename string) *AllStructs {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filename, file, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	pkgName := node.Name.Name
	ss := AllStructs{pkgName: map[string]*Struct{}}
	for _, decl := range node.Decls {
		switch decl := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structName := ts.Name.Name
				s := ss[pkgName][structName]
				if s == nil {
					s = &Struct{}
				}

				switch typ := ts.Type.(type) {
				case *ast.StructType:
					s.Type = typeStruct
					s.Package = pkgName
					s.Name = ts.Name.Name
				case *ast.InterfaceType:
					s.Type = typeInterface
					s.Package = pkgName
					s.Name = ts.Name.Name
					methods := getInterfaceMethods(typ)
					s.Methods = methods
				default:
					log.Printf("unknown type %T", typ)
				}

				ss[pkgName][structName] = s
			}
		case *ast.FuncDecl:
			recv := decl.Recv
			if recv == nil {
				continue // レシーバなしの関数は無視
			}

			recvType := recv.List[0].Type.(*ast.StarExpr).X.(*ast.Ident).Name
			methodName := decl.Name.Name
			args := decl.Type.Params
			argsTypes := []string{}
			for _, arg := range args.List {
				argsTypes = append(argsTypes, arg.Type.(*ast.Ident).Name)
			}

			returns := decl.Type.Results
			returnTypes := []string{}
			if returns != nil {
				for _, ret := range decl.Type.Results.List {
					returnTypes = append(returnTypes, ret.Type.(*ast.Ident).Name)
				}
			}
			method := Method{
				Name:    methodName,
				Args:    argsTypes,
				Returns: returnTypes,
			}
			if ss[pkgName][recvType] == nil {
				ss[pkgName][recvType] = &Struct{
					Type:    typeStruct,
					Package: pkgName,
					Name:    recvType,
				}
			}
			ss[pkgName][recvType].AddMethod(&method)
		}
	}

	return &ss
}

type Method struct {
	Name    string
	Args    []string
	Returns []string
}

func getInterfaceMethods(typ *ast.InterfaceType) []*Method {
	methods := []*Method{}
	for _, m := range typ.Methods.List {
		method := Method{}
		for _, name := range m.Names {
			method.Name = name.Name
		}
		args := m.Type.(*ast.FuncType).Params
		for _, arg := range args.List {
			method.Args = append(method.Args, arg.Type.(*ast.Ident).Name)
		}
		returns := m.Type.(*ast.FuncType).Results
		if returns != nil {
			for _, ret := range returns.List {
				method.Returns = append(method.Returns, ret.Type.(*ast.Ident).Name)
			}
		}
		methods = append(methods, &method)
	}
	return methods
}

type expInterface struct {
	Name    string `json:"name"`
	Package string `json:"package"`
}

func insertInterface(ctx context.Context, driver neo4j.DriverWithContext, i *expInterface) (*expInterface, error) {
	session := driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	return neo4j.ExecuteWrite(ctx, session, createInterfaceFn(ctx, i))
}

func createInterfaceFn(ctx context.Context, i *expInterface) neo4j.ManagedTransactionWorkT[*expInterface] {
	return func(tx neo4j.ManagedTransaction) (*expInterface, error) {
		_, err := tx.Run(ctx, "CREATE (i:Interface {name: $name, package: $package}) RETURN i", map[string]any{"name": i.Name, "package": i.Package})
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
}

type expStruct struct {
	Name    string `json:"name"`
	Package string `json:"package"`
}

func insertStruct(ctx context.Context, driver neo4j.DriverWithContext, s *expStruct) (*expStruct, error) {
	session := driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	return neo4j.ExecuteWrite(ctx, session, createStructFn(ctx, s))
}

func createStructFn(ctx context.Context, s *expStruct) neo4j.ManagedTransactionWorkT[*expStruct] {
	return func(tx neo4j.ManagedTransaction) (*expStruct, error) {
		_, err := tx.Run(ctx, "CREATE (s:Struct {name: $name, package: $package}) RETURN s", map[string]any{"name": s.Name, "package": s.Package})
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
}

type expPackage struct {
	Name string `json:"name"`
}

func insertPackage(ctx context.Context, driver neo4j.DriverWithContext, p *expPackage) (*expPackage, error) {
	session := driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	return neo4j.ExecuteWrite(ctx, session, createPackageFn(ctx, p))
}

func createPackageFn(ctx context.Context, p *expPackage) neo4j.ManagedTransactionWorkT[*expPackage] {
	return func(tx neo4j.ManagedTransaction) (*expPackage, error) {
		_, err := tx.Run(ctx, "CREATE (p:Package {name: $name}) RETURN p", map[string]any{"name": p.Name})
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func renderFile(ss *AllStructs) {
	dbUri := "neo4j://localhost"
	driver, err := neo4j.NewDriverWithContext(dbUri, neo4j.BasicAuth("neo4j", "hogehoge", ""))
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	defer driver.Close(ctx)

	for pkgName, structs := range *ss {
		_, err := insertPackage(ctx, driver, &expPackage{Name: pkgName})
		if err != nil {
			log.Fatal(err)
		}
		for structName, s := range structs {
			switch s.Type {
			case typeStruct:
				insertStruct(ctx, driver, &expStruct{Name: structName, Package: pkgName})
			case typeInterface:
				insertInterface(ctx, driver, &expInterface{Name: structName, Package: pkgName})
			}
		}
	}
}

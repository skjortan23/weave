package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/iov-one/weave/cmd/bnsd/client"
	"github.com/iov-one/weave/orm"
	"github.com/iov-one/weave/x/gov"
)

func cmdQuery(input io.Reader, output io.Writer, args []string) error {
	fl := flag.NewFlagSet("", flag.ExitOnError)
	fl.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), `
Query. TODO
`)
		fl.PrintDefaults()
	}
	var (
		tmAddrFl = fl.String("tm", env("BNSCLI_TM_ADDR", "https://bns.NETWORK.iov.one:443"),
			"Tendermint node address. Use proper NETWORK name. You can use BNSCLI_TM_ADDR environment variable to set it.")
	)
	fl.Parse(args)

	bnsClient := client.NewClient(client.NewHTTPConnection(*tmAddrFl))

	var (
		path string
		data []byte
	)
	switch len(fl.Args()) {
	case 1:
		// Find many.
		path = fl.Args()[0] + "?prefix"
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	case 2:
		// Find one by ID.
		path = fl.Args()[0]
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		data = []byte(fl.Args()[1])

	default:
		flagDie("bad command usage")
	}

	queryName := strings.Split(path, "?")[0]
	spec, ok := queries[queryName]
	if !ok {
		writeAvailablePaths(output)
		return fmt.Errorf("unknown query path %q", queryName)
	}

	resp, err := bnsClient.AbciQuery(path, data)
	if err != nil {
		return fmt.Errorf("cannot query: %s", err)
	}
	if len(resp.Models) == 0 {
		return nil
	}

	var entities []entity
	for i, rawModel := range resp.Models {
		model := reflect.New(spec.tp).Interface().(orm.Model)
		if err := model.Unmarshal(rawModel.Value); err != nil {
			return fmt.Errorf("cannot unmarshal #%d model: %s", i, err)
		}
		entities = append(entities, entity{
			ID:    int64(binary.BigEndian.Uint64(rawModel.Key[len(spec.tp.Name())+1:])),
			Key:   rawModel.Key,
			Name:  spec.tp.Name(),
			Value: model,
		})
	}

	pretty, err := json.MarshalIndent(entities, "", "\t")
	if err != nil {
		return fmt.Errorf("cannot JSON marshal entities: %s", err)
	}
	_, _ = output.Write(pretty)

	return nil
}

type entity struct {
	ID    int64
	Key   []byte
	Name  string
	Value orm.Model
}

// Queries provides a mapping of the query path to the type of the data it
// returns.
var queries = map[string]struct {
	desc string
	tp   reflect.Type
}{
	"/proposals": {
		tp: reflect.TypeOf(gov.Proposal{}),
	},
	"/votes": {
		tp: reflect.TypeOf(gov.Vote{}),
	},
	"/votes/electors": {
		tp: reflect.TypeOf(gov.Vote{}),
	},
	"/votes/proposals": {
		tp: reflect.TypeOf(gov.Vote{}),
	},
	"/electorates": {
		tp: reflect.TypeOf(gov.Electorate{}),
	},
	"/electionrules": {
		tp: reflect.TypeOf(gov.ElectionRule{}),
	},
	"/resolutions": {
		tp: reflect.TypeOf(gov.Resolution{}),
	},
}

func writeAvailablePaths(out io.Writer) {
	fmt.Fprintln(out, "Available query paths and returned model type:")
	for path, spec := range queries {
		fmt.Fprintf(out, "\t%s\t%s %s\n", path, spec.tp.Name(), spec.desc)
	}
}

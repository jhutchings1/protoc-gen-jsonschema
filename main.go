// protoc plugin which converts .proto to JSON schema
// It is spawned by protoc and generates JSON-schema files.
// "Heavily influenced" by Google's "protog-gen-bq-schema"
//
// usage:
//  $ bin/protoc --jsonschema_out=path/to/outdir foo.proto
//
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"

	"github.com/alecthomas/jsonschema"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	"github.com/xeipuuv/gojsonschema"
)

const (
	LOG_DEBUG = 0
	LOG_INFO  = 1
	LOG_WARN  = 2
	LOG_ERROR = 3
	LOG_FATAL = 4
	LOG_PANIC = 5
)

var (
	allowNullValues              bool
	disallowEnumOneOf            bool
	disallowOneOf                bool
	disallowAdditionalProperties bool
	disallowBigIntsAsStrings     bool
	debugLogging                 bool
	globalPkg                    = &ProtoPackage{
		name:     "",
		parent:   nil,
		children: make(map[string]*ProtoPackage),
		types:    make(map[string]*descriptor.DescriptorProto),
	}
	logLevels = map[LogLevel]string{
		0: "DEBUG",
		1: "INFO",
		2: "WARN",
		3: "ERROR",
		4: "FATAL",
		5: "PANIC",
	}
)

// ProtoPackage describes a package of Protobuf, which is an container of message types.
type ProtoPackage struct {
	name     string
	parent   *ProtoPackage
	children map[string]*ProtoPackage
	types    map[string]*descriptor.DescriptorProto
}

type LogLevel int

func init() {
	flag.BoolVar(&allowNullValues, "allow_null_values", false, "Allow NULL values to be validated")
	flag.BoolVar(&disallowEnumOneOf, "disallow_enum_one_of", false, "Disallows enums to have number value as well as name value")
	flag.BoolVar(&disallowOneOf, "disallow_one_of", false, "Disallows oneOf types")
	flag.BoolVar(&disallowAdditionalProperties, "disallow_additional_properties", false, "Disallow additional properties")
	flag.BoolVar(&disallowBigIntsAsStrings, "disallow_bigints_as_strings", false, "Disallow bigints to be strings (eg scientific notation)")
	flag.BoolVar(&debugLogging, "debug", false, "Log debug messages")
}

func logWithLevel(logLevel LogLevel, logFormat string, logParams ...interface{}) {
	// If we're not doing debug logging then just return:
	if logLevel <= LOG_INFO && !debugLogging {
		return
	}

	// Otherwise log:
	logMessage := fmt.Sprintf(logFormat, logParams...)
	log.Printf(fmt.Sprintf("[%v] %v", logLevels[logLevel], logMessage))
}

func registerType(pkgName *string, msg *descriptor.DescriptorProto) {
	pkg := globalPkg
	if pkgName != nil {
		for _, node := range strings.Split(*pkgName, ".") {
			if pkg == globalPkg && node == "" {
				// Skips leading "."
				continue
			}
			child, ok := pkg.children[node]
			if !ok {
				child = &ProtoPackage{
					name:     pkg.name + "." + node,
					parent:   pkg,
					children: make(map[string]*ProtoPackage),
					types:    make(map[string]*descriptor.DescriptorProto),
				}
				pkg.children[node] = child
			}
			pkg = child
		}
	}
	pkg.types[msg.GetName()] = msg
}

func (pkg *ProtoPackage) lookupType(name string) (*descriptor.DescriptorProto, bool) {
	if strings.HasPrefix(name, ".") {
		return globalPkg.relativelyLookupType(name[1:len(name)])
	}

	for ; pkg != nil; pkg = pkg.parent {
		if desc, ok := pkg.relativelyLookupType(name); ok {
			return desc, ok
		}
	}
	return nil, false
}

func relativelyLookupNestedType(desc *descriptor.DescriptorProto, name string) (*descriptor.DescriptorProto, bool) {
	components := strings.Split(name, ".")
componentLoop:
	for _, component := range components {
		for _, nested := range desc.GetNestedType() {
			if nested.GetName() == component {
				desc = nested
				continue componentLoop
			}
		}
		logWithLevel(LOG_INFO, "no such nested message %s in %s", component, desc.GetName())
		return nil, false
	}
	return desc, true
}

func (pkg *ProtoPackage) relativelyLookupType(name string) (*descriptor.DescriptorProto, bool) {
	components := strings.SplitN(name, ".", 2)
	switch len(components) {
	case 0:
		logWithLevel(LOG_DEBUG, "empty message name")
		return nil, false
	case 1:
		found, ok := pkg.types[components[0]]
		return found, ok
	case 2:
		logWithLevel(LOG_DEBUG, "looking for %s in %s at %s (%v)", components[1], components[0], pkg.name, pkg)
		if child, ok := pkg.children[components[0]]; ok {
			found, ok := child.relativelyLookupType(components[1])
			return found, ok
		}
		if msg, ok := pkg.types[components[0]]; ok {
			found, ok := relativelyLookupNestedType(msg, components[1])
			return found, ok
		}
		logWithLevel(LOG_INFO, "no such package nor message %s in %s", components[0], pkg.name)
		return nil, false
	default:
		logWithLevel(LOG_FATAL, "not reached")
		return nil, false
	}
}

func (pkg *ProtoPackage) relativelyLookupPackage(name string) (*ProtoPackage, bool) {
	components := strings.Split(name, ".")
	for _, c := range components {
		var ok bool
		pkg, ok = pkg.children[c]
		if !ok {
			return nil, false
		}
	}
	return pkg, true
}

// Convert a proto "field" (essentially a type-switch with some recursion):
func convertField(curPkg *ProtoPackage, desc *descriptor.FieldDescriptorProto, msg *descriptor.DescriptorProto) (*jsonschema.Type, error) {
	// Helpers for this inverse logic shit
	allowEnumOneOf := !disallowEnumOneOf
	allowOneOf := !disallowOneOf

	// Prepare a new jsonschema.Type for our eventual return value:
	jsonSchemaType := &jsonschema.Type{
		Properties: make(map[string]*jsonschema.Type),
	}

	// Switch the types, and pick a JSONSchema equivalent:
	switch desc.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE,
		descriptor.FieldDescriptorProto_TYPE_FLOAT:
		if allowNullValues && allowOneOf {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_NUMBER},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_NUMBER
		}

	case descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_FIXED32,
		descriptor.FieldDescriptorProto_TYPE_SFIXED32,
		descriptor.FieldDescriptorProto_TYPE_SINT32:
		if allowNullValues && allowOneOf {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_INTEGER},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_INTEGER
		}

	case descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT64,
		descriptor.FieldDescriptorProto_TYPE_FIXED64,
		descriptor.FieldDescriptorProto_TYPE_SFIXED64,
		descriptor.FieldDescriptorProto_TYPE_SINT64:

		if allowOneOf {
			jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_INTEGER})
			if !disallowBigIntsAsStrings {
				jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_STRING})
			}
			if allowNullValues {
				jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_NULL})
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_INTEGER
		}

	case descriptor.FieldDescriptorProto_TYPE_STRING,
		descriptor.FieldDescriptorProto_TYPE_BYTES:
		if allowNullValues && allowOneOf {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_STRING},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_STRING
		}

	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		// NOTE with the original way this library worked (no concept of `allowEnumOneOf`), enums could pass validation with either
		// the integer or the string passed in. Well, in the down stream processes (like data lake) these fields are expected to
		// actually be the string representation. So in something like data lake, the value for the enum column would be the string
		// or the number enum representation of that string. Therefore, we must only allow the string and not the number to be sent

		if allowEnumOneOf && allowOneOf {
			jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_STRING})
			jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_INTEGER})

			if allowNullValues {
				jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: gojsonschema.TYPE_NULL})
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_STRING
		}

		foundEnum := false
		// Go through all the enums we have, see if we can match any to this field by name:
		for _, enumDescriptor := range msg.GetEnumType() {

			// Is this the enum we care about?
			if foundEnum || !strings.HasSuffix(desc.GetTypeName(), *enumDescriptor.Name) {
				continue
			}

			// Indicate we found what we are looking for
			foundEnum = true

			// Each one has several values:
			for _, enumValue := range enumDescriptor.Value {

				// Put the ENUM values into the JSONSchema list of allowed ENUM values:
				jsonSchemaType.Enum = append(jsonSchemaType.Enum, enumValue.Name)

				// NOTE if we are going to allow oneOf, then we should just stick to the default way
				if allowEnumOneOf {
					jsonSchemaType.Enum = append(jsonSchemaType.Enum, enumValue.Number)
				}
			}
		}

		if !foundEnum {
			logWithLevel(LOG_WARN, "could not find matching enum for field %s with type %s", *desc.Name, *desc.TypeName)
		}

	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		if allowNullValues && allowOneOf {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_BOOLEAN},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_BOOLEAN
		}

	case descriptor.FieldDescriptorProto_TYPE_GROUP,
		descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		switch desc.GetTypeName() {
		case ".google.protobuf.Timestamp":
			jsonSchemaType.Type = gojsonschema.TYPE_STRING
			jsonSchemaType.Format = "date-time"
		default:
			jsonSchemaType.Type = gojsonschema.TYPE_OBJECT
			if disallowAdditionalProperties {
				jsonSchemaType.AdditionalProperties = []byte("false")
			} else {
				if desc.GetLabel() == descriptor.FieldDescriptorProto_LABEL_OPTIONAL {
					jsonSchemaType.AdditionalProperties = []byte("true")
				}
				if desc.GetLabel() == descriptor.FieldDescriptorProto_LABEL_REQUIRED {
					jsonSchemaType.AdditionalProperties = []byte("false")
				}
			}
		}

	default:
		return nil, fmt.Errorf("unrecognized field type: %s", desc.GetType().String())
	}

	// Recurse array of primitive types:
	if desc.GetLabel() == descriptor.FieldDescriptorProto_LABEL_REPEATED && jsonSchemaType.Type != gojsonschema.TYPE_OBJECT {
		jsonSchemaType.Items = &jsonschema.Type{}

		if len(jsonSchemaType.Enum) > 0 {
			jsonSchemaType.Items.Enum = jsonSchemaType.Enum
			jsonSchemaType.Enum = nil

			if allowEnumOneOf && allowOneOf {
				jsonSchemaType.Items.OneOf = jsonSchemaType.OneOf
			} else {
				jsonSchemaType.Items.Type = jsonSchemaType.Type
			}
		} else {
			jsonSchemaType.Items.Type = jsonSchemaType.Type
			jsonSchemaType.Items.OneOf = jsonSchemaType.OneOf
		}

		if allowNullValues && allowOneOf {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: gojsonschema.TYPE_ARRAY},
			}
		} else {
			jsonSchemaType.Type = gojsonschema.TYPE_ARRAY
			jsonSchemaType.OneOf = []*jsonschema.Type{}
		}

		return jsonSchemaType, nil
	}

	// Recurse nested objects / arrays of objects (if necessary):
	if jsonSchemaType.Type == gojsonschema.TYPE_OBJECT {

		recordType, ok := curPkg.lookupType(desc.GetTypeName())
		if !ok {
			return nil, fmt.Errorf("no such message type named %s", desc.GetTypeName())
		}

		// C. Locklear -- I think we need to add all the enums from msg into recordType here
		if len(recordType.EnumType) == 0 {
			for _, d := range msg.EnumType {
				recordType.EnumType = append(recordType.EnumType, d)
			}
		}
		// Recurse:
		recursedJSONSchemaType, err := convertMessageType(curPkg, recordType)
		if err != nil {
			return nil, err
		}

		// The result is stored differently for arrays of objects (they become "items"):
		if desc.GetLabel() == descriptor.FieldDescriptorProto_LABEL_REPEATED {
			jsonSchemaType.Items = &recursedJSONSchemaType
			jsonSchemaType.Type = gojsonschema.TYPE_ARRAY
		} else {
			// Nested objects are more straight-forward:
			jsonSchemaType.Properties = recursedJSONSchemaType.Properties
		}

		// Optionally allow NULL values:
		if allowNullValues && allowOneOf {
			jsonSchemaType.OneOf = []*jsonschema.Type{
				{Type: gojsonschema.TYPE_NULL},
				{Type: jsonSchemaType.Type},
			}
			jsonSchemaType.Type = ""
		}
	}

	return jsonSchemaType, nil
}

// Converts a proto "MESSAGE" into a JSON-Schema:
func convertMessageType(curPkg *ProtoPackage, msg *descriptor.DescriptorProto) (jsonschema.Type, error) {
	// Helpers for this inverse logic shit
	allowOneOf := !disallowOneOf

	// Prepare a new jsonschema:
	jsonSchemaType := jsonschema.Type{
		Properties: make(map[string]*jsonschema.Type),
		Version:    jsonschema.Version,
	}

	// Optionally allow NULL values:
	if allowNullValues && allowOneOf {
		jsonSchemaType.OneOf = []*jsonschema.Type{
			{Type: gojsonschema.TYPE_NULL},
			{Type: gojsonschema.TYPE_OBJECT},
		}
	} else {
		jsonSchemaType.Type = gojsonschema.TYPE_OBJECT
	}

	// disallowAdditionalProperties will prevent validation where extra fields are found (outside of the schema):
	if disallowAdditionalProperties {
		jsonSchemaType.AdditionalProperties = []byte("false")
	} else {
		jsonSchemaType.AdditionalProperties = []byte("true")
	}

	logWithLevel(LOG_DEBUG, "Converting message: %s", proto.MarshalTextString(msg))
	for _, fieldDesc := range msg.GetField() {
		recursedJSONSchemaType, err := convertField(curPkg, fieldDesc, msg)
		if err != nil {
			logWithLevel(LOG_ERROR, "Failed to convert field %s in %s: %v", fieldDesc.GetName(), msg.GetName(), err)
			return jsonSchemaType, err
		}
		jsonSchemaType.Properties[fieldDesc.GetJsonName()] = recursedJSONSchemaType
	}
	return jsonSchemaType, nil
}

// Converts a proto "ENUM" into a JSON-Schema:
func convertEnumType(enum *descriptor.EnumDescriptorProto) (jsonschema.Type, error) {
	// Helpers for this inverse logic shit
	allowEnumOneOf := !disallowEnumOneOf
	allowOneOf := !disallowOneOf

	// Prepare a new jsonschema.Type for our eventual return value:
	jsonSchemaType := jsonschema.Type{
		Version: jsonschema.Version,
	}

	if allowEnumOneOf && allowOneOf {
		// Allow both strings and integers:
		jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: "string"})
		jsonSchemaType.OneOf = append(jsonSchemaType.OneOf, &jsonschema.Type{Type: "integer"})
	} else {
		jsonSchemaType.Type = gojsonschema.TYPE_STRING
	}

	// Add the allowed values:
	for _, enumValue := range enum.Value {
		jsonSchemaType.Enum = append(jsonSchemaType.Enum, enumValue.Name)

		if allowEnumOneOf && allowOneOf {
			jsonSchemaType.Enum = append(jsonSchemaType.Enum, enumValue.Number)
		}
	}

	return jsonSchemaType, nil
}

// Converts a proto file into a JSON-Schema:
func convertFile(file *descriptor.FileDescriptorProto) ([]*plugin.CodeGeneratorResponse_File, error) {

	// Input filename:
	protoFileName := path.Base(file.GetName())

	// Prepare a list of responses:
	response := []*plugin.CodeGeneratorResponse_File{}

	// Warn about multiple messages / enums in files:
	if len(file.GetMessageType()) > 1 {
		logWithLevel(LOG_WARN, "protoc-gen-jsonschema will create multiple MESSAGE schemas (%d) from one proto file (%v)", len(file.GetMessageType()), protoFileName)
	}
	if len(file.GetEnumType()) > 1 {
		logWithLevel(LOG_WARN, "protoc-gen-jsonschema will create multiple ENUM schemas (%d) from one proto file (%v)", len(file.GetEnumType()), protoFileName)
	}

	// Generate standalone ENUMs:
	if len(file.GetMessageType()) == 0 {
		for _, enum := range file.GetEnumType() {
			jsonSchemaFileName := fmt.Sprintf("%s.jsonschema", enum.GetName())
			logWithLevel(LOG_INFO, "Generating JSON-schema for stand-alone ENUM (%v) in file [%v] => %v", enum.GetName(), protoFileName, jsonSchemaFileName)
			enumJsonSchema, err := convertEnumType(enum)
			if err != nil {
				logWithLevel(LOG_ERROR, "Failed to convert %s: %v", protoFileName, err)
				return nil, err
			} else {
				// Marshal the JSON-Schema into JSON:
				jsonSchemaJSON, err := json.MarshalIndent(enumJsonSchema, "", "    ")
				if err != nil {
					logWithLevel(LOG_ERROR, "Failed to encode jsonSchema: %v", err)
					return nil, err
				} else {
					// Add a response:
					resFile := &plugin.CodeGeneratorResponse_File{
						Name:    proto.String(jsonSchemaFileName),
						Content: proto.String(string(jsonSchemaJSON)),
					}
					response = append(response, resFile)
				}
			}
		}
	} else {
		// Otherwise process MESSAGES (packages):
		pkg, ok := globalPkg.relativelyLookupPackage(file.GetPackage())
		if !ok {
			return nil, fmt.Errorf("no such package found: %s", file.GetPackage())
		}
		for _, msg := range file.GetMessageType() {
			jsonSchemaFileName := fmt.Sprintf("%s.jsonschema", msg.GetName())
			logWithLevel(LOG_INFO, "Generating JSON-schema for MESSAGE (%v) in file [%v] => %v", msg.GetName(), protoFileName, jsonSchemaFileName)
			// C. Locklear -- Let's send any ENUMs we know about into this msg so that
			// we can find them when we build our JSON schema.  This will solve the scenario
			// that arises when an enum is used in message, defined outside the message, but
			// in the same file.
			for _, v := range file.EnumType {
				msg.EnumType = append(msg.EnumType, v)
			}
			messageJSONSchema, err := convertMessageType(pkg, msg)
			if err != nil {
				logWithLevel(LOG_ERROR, "Failed to convert %s: %v", protoFileName, err)
				return nil, err
			} else {
				// Marshal the JSON-Schema into JSON:
				jsonSchemaJSON, err := json.MarshalIndent(messageJSONSchema, "", "    ")
				if err != nil {
					logWithLevel(LOG_ERROR, "Failed to encode jsonSchema: %v", err)
					return nil, err
				} else {
					// Add a response:
					resFile := &plugin.CodeGeneratorResponse_File{
						Name:    proto.String(jsonSchemaFileName),
						Content: proto.String(string(jsonSchemaJSON)),
					}
					response = append(response, resFile)
				}
			}
		}
	}

	return response, nil
}

func convert(req *plugin.CodeGeneratorRequest) (*plugin.CodeGeneratorResponse, error) {
	generateTargets := make(map[string]bool)
	for _, file := range req.GetFileToGenerate() {
		generateTargets[file] = true
	}

	res := &plugin.CodeGeneratorResponse{}
	enumDescriptors := make([]*descriptor.EnumDescriptorProto, 0)
	for _, file := range req.GetProtoFile() {
		for _, msg := range file.GetMessageType() {
			logWithLevel(LOG_DEBUG, "Loading a message type %s from package %s", msg.GetName(), file.GetPackage())
			registerType(file.Package, msg)
		}
		// Gather all the enum descriptors referenced across all files.
		// We're going to inject them into our converter to better improve
		// the chances that external enums are properly converted in our
		// new JSON schemas.
		for _, d := range file.EnumType {
			enumDescriptors = append(enumDescriptors, d)
		}
	}
	for _, file := range req.GetProtoFile() {
		if _, ok := generateTargets[file.GetName()]; ok {
			logWithLevel(LOG_DEBUG, "Converting file (%v)", file.GetName())
			// Swapparoo
			file.EnumType = enumDescriptors
			converted, err := convertFile(file)
			if err != nil {
				res.Error = proto.String(fmt.Sprintf("Failed to convert %s: %v", file.GetName(), err))
				return res, err
			}
			res.File = append(res.File, converted...)
		}
	}
	return res, nil
}

func convertFrom(rd io.Reader) (*plugin.CodeGeneratorResponse, error) {
	logWithLevel(LOG_DEBUG, "Reading code generation request")
	input, err := ioutil.ReadAll(rd)
	if err != nil {
		logWithLevel(LOG_ERROR, "Failed to read request: %v", err)
		return nil, err
	}

	req := &plugin.CodeGeneratorRequest{}
	err = proto.Unmarshal(input, req)
	if err != nil {
		logWithLevel(LOG_ERROR, "Can't unmarshal input: %v", err)
		return nil, err
	}

	commandLineParameter(req.GetParameter())

	logWithLevel(LOG_DEBUG, "Converting input")
	return convert(req)
}

func commandLineParameter(parameters string) {
	for _, parameter := range strings.Split(parameters, ",") {
		switch parameter {
		case "allow_null_values":
			allowNullValues = true
		case "debug":
			debugLogging = true
		case "disallow_enum_one_of":
			disallowEnumOneOf = true
		case "disallow_one_of":
			if allowNullValues {
				panic("flags 'allow_null_values' and 'disallow_one_of' cannot both be on")
			}

			disallowOneOf = true
		case "disallow_additional_properties":
			disallowAdditionalProperties = true
		case "disallow_bigints_as_strings":
			disallowBigIntsAsStrings = true
		}
	}
}

func main() {
	flag.Parse()
	ok := true
	logWithLevel(LOG_DEBUG, "Processing code generator request")
	res, err := convertFrom(os.Stdin)
	if err != nil {
		ok = false
		if res == nil {
			message := fmt.Sprintf("Failed to read input: %v", err)
			res = &plugin.CodeGeneratorResponse{
				Error: &message,
			}
		}
	}

	logWithLevel(LOG_DEBUG, "Serializing code generator response")
	data, err := proto.Marshal(res)
	if err != nil {
		logWithLevel(LOG_FATAL, "Cannot marshal response: %v", err)
	}
	_, err = os.Stdout.Write(data)
	if err != nil {
		logWithLevel(LOG_FATAL, "Failed to write response: %v", err)
	}

	if ok {
		logWithLevel(LOG_DEBUG, "Succeeded to process code generator request")
	} else {
		logWithLevel(LOG_WARN, "Failed to process code generator but successfully sent the error to protoc")
		os.Exit(1)
	}
}

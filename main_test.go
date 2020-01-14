package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/chrusty/protoc-gen-jsonschema/testdata"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var (
	protocBinary         = "/bin/protoc"
	sampleProtoDirectory = "testdata/proto"
	sampleProtos         = make(map[string]SampleProto)
)

type SampleProto struct {
	AllowNullValues    bool
	DisallowEnumOneOf  bool
	DisallowOneOf      bool
	DisallowAdditional bool
	ExpectedJsonSchema []string
	FilesToGenerate    []string
	ProtoFileName      string
}

func TestGenerateJsonSchema(t *testing.T) {
	// We only want to see "Info" level logs and above (there's a LOT of debug otherwise):
	log.SetLevel(log.InfoLevel)

	// Make sure we have "protoc" installed and available:
	testForProtocBinary(t)

	// Configure the list of sample protos to test, and their expected JSON-Schemas:
	configureSampleProtos()

	// Convert the protos, compare the results against the expected JSON-Schemas:
	testConvertSampleProtos(t, sampleProtos["ArrayOfMessages"])
	testConvertSampleProtos(t, sampleProtos["ArrayOfObjects"])
	testConvertSampleProtos(t, sampleProtos["ArrayOfPrimitives"])
	testConvertSampleProtos(t, sampleProtos["EnumCeption"])
	testConvertSampleProtos(t, sampleProtos["EnumWithNoOneOf"])
	testConvertSampleProtos(t, sampleProtos["ExternalEnum"])
	testConvertSampleProtos(t, sampleProtos["ImportedExternalEnum"])
	testConvertSampleProtos(t, sampleProtos["ImportedEnum"])
	testConvertSampleProtos(t, sampleProtos["NestedMessage"])
	testConvertSampleProtos(t, sampleProtos["NestedMessageNoAdditionalProperties"])
	testConvertSampleProtos(t, sampleProtos["NestedObject"])
	testConvertSampleProtos(t, sampleProtos["NoOneOf"])
	testConvertSampleProtos(t, sampleProtos["PayloadMessage"])
	testConvertSampleProtos(t, sampleProtos["SeveralEnums"])
	testConvertSampleProtos(t, sampleProtos["SeveralMessages"])
	testConvertSampleProtos(t, sampleProtos["ArrayOfEnums"])
	testConvertSampleProtos(t, sampleProtos["Timestamp"])
}

func testForProtocBinary(t *testing.T) {
	path, err := exec.LookPath("protoc")
	if err != nil {
		assert.NoError(t, err, "Can't find 'protoc' binary in $PATH")
	} else {
		protocBinary = path
		log.Infof("Found 'protoc' binary (%v)", protocBinary)
	}
}

func testConvertSampleProtos(t *testing.T, sampleProto SampleProto) {

	// Set allowNullValues accordingly:
	allowNullValues = sampleProto.AllowNullValues
	disallowEnumOneOf = sampleProto.DisallowEnumOneOf
	disallowOneOf = sampleProto.DisallowOneOf
	disallowAdditionalProperties = sampleProto.DisallowAdditional

	// Open the sample proto file:
	sampleProtoFileName := fmt.Sprintf("%v/%v", sampleProtoDirectory, sampleProto.ProtoFileName)

	// Prepare to run the "protoc" command (generates a CodeGeneratorRequest):
	protocCommand := exec.Command(protocBinary, "--descriptor_set_out=/dev/stdout", "--include_imports", fmt.Sprintf("--proto_path=%v", sampleProtoDirectory), sampleProtoFileName)
	var protocCommandOutput bytes.Buffer
	errChan := &bytes.Buffer{}
	protocCommand.Stdout = &protocCommandOutput
	protocCommand.Stderr = errChan
	// Run the command:
	err := protocCommand.Run()
	assert.NoError(t, err, "Unable to prepare a codeGeneratorRequest using protoc (%v) for sample proto file (%v) (%s)", protocBinary, sampleProtoFileName, strings.TrimSpace(errChan.String()))

	// Unmarshal the output from the protoc command (should be a "FileDescriptorSet"):
	fileDescriptorSet := new(descriptor.FileDescriptorSet)
	err = proto.Unmarshal(protocCommandOutput.Bytes(), fileDescriptorSet)
	assert.NoError(t, err, "Unable to unmarshal proto FileDescriptorSet for sample proto file (%v)", sampleProtoFileName)

	// Prepare a request:
	codeGeneratorRequest := plugin.CodeGeneratorRequest{
		FileToGenerate: sampleProto.FilesToGenerate,
		ProtoFile:      fileDescriptorSet.GetFile(),
	}

	// Perform the conversion:
	response, err := convert(&codeGeneratorRequest)
	assert.NoError(t, err, "Unable to convert sample proto file (%v)", sampleProtoFileName)
	assert.Equal(t, len(sampleProto.ExpectedJsonSchema), len(response.File), "Incorrect number of JSON-Schema files returned for sample proto file (%v)", sampleProtoFileName)
	if len(sampleProto.ExpectedJsonSchema) != len(response.File) {
		t.Fail()
	} else {
		for responseFileIndex, responseFile := range response.File {
			assert.Equal(t, sampleProto.ExpectedJsonSchema[responseFileIndex], *responseFile.Content, "Incorrect JSON-Schema returned for sample proto file (%v)", sampleProtoFileName)
		}
	}
}

func configureSampleProtos() {
	// ArrayOfMessages:
	sampleProtos["ArrayOfMessages"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.PayloadMessage, testdata.ArrayOfMessages},
		FilesToGenerate:    []string{"ArrayOfMessages.proto", "PayloadMessage.proto"},
		ProtoFileName:      "ArrayOfMessages.proto",
	}

	// ArrayOfEnums
	sampleProtos["ArrayOfEnums"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.ArrayOfEnums},
		FilesToGenerate:    []string{"ArrayOfEnums.proto"},
		ProtoFileName:      "ArrayOfEnums.proto",
	}

	// ArrayOfObjects:
	sampleProtos["ArrayOfObjects"] = SampleProto{
		AllowNullValues:    true,
		ExpectedJsonSchema: []string{testdata.ArrayOfObjects},
		FilesToGenerate:    []string{"ArrayOfObjects.proto"},
		ProtoFileName:      "ArrayOfObjects.proto",
	}

	// ArrayOfPrimitives:
	sampleProtos["ArrayOfPrimitives"] = SampleProto{
		AllowNullValues:    true,
		ExpectedJsonSchema: []string{testdata.ArrayOfPrimitives},
		FilesToGenerate:    []string{"ArrayOfPrimitives.proto"},
		ProtoFileName:      "ArrayOfPrimitives.proto",
	}

	// EnumCeption:
	sampleProtos["EnumCeption"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.PayloadMessage, testdata.ImportedEnum, testdata.EnumCeption},
		FilesToGenerate:    []string{"Enumception.proto", "PayloadMessage.proto", "ImportedEnum.proto"},
		ProtoFileName:      "Enumception.proto",
	}

	// EnumWithNoOneOf:
	sampleProtos["EnumWithNoOneOf"] = SampleProto{
		AllowNullValues:    false,
		DisallowEnumOneOf:  true,
		DisallowOneOf:      true,
		ExpectedJsonSchema: []string{testdata.EnumWithNoOneOf},
		FilesToGenerate:    []string{"EnumWithNoOneOf.proto"},
		ProtoFileName:      "EnumWithNoOneOf.proto",
	}

	// ExternalEnum:
	sampleProtos["ExternalEnum"] = SampleProto{
		AllowNullValues:    false,
		DisallowEnumOneOf:  true,
		DisallowOneOf:      true,
		ExpectedJsonSchema: []string{testdata.ExternalEnum},
		FilesToGenerate:    []string{"ExternalEnum.proto"},
		ProtoFileName:      "ExternalEnum.proto",
	}

	// ImportedEnum:
	sampleProtos["ImportedEnum"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.ImportedEnum},
		FilesToGenerate:    []string{"ImportedEnum.proto"},
		ProtoFileName:      "ImportedEnum.proto",
	}

	// ImportedExternalEnum:
	sampleProtos["ImportedExternalEnum"] = SampleProto{
		AllowNullValues:    false,
		DisallowEnumOneOf:  true,
		DisallowOneOf:      true,
		ExpectedJsonSchema: []string{testdata.ImportedExternalEnum},
		FilesToGenerate:    []string{"ImportedExternalEnum.proto"},
		ProtoFileName:      "ImportedExternalEnum.proto",
	}

	// NestedMessage:
	sampleProtos["NestedMessage"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.PayloadMessage, testdata.NestedMessage},
		FilesToGenerate:    []string{"NestedMessage.proto", "PayloadMessage.proto"},
		ProtoFileName:      "NestedMessage.proto",
	}

	// NestedMessageNoAdditionalProperties:
	sampleProtos["NestedMessageNoAdditionalProperties"] = SampleProto{
		AllowNullValues:    false,
		DisallowAdditional: true,
		ExpectedJsonSchema: []string{testdata.PayloadMessageNoAdditionalProperties, testdata.NestedMessageNoAdditionalProperties},
		FilesToGenerate:    []string{"NestedMessage.proto", "PayloadMessage.proto"},
		ProtoFileName:      "NestedMessage.proto",
	}

	// NestedObject:
	sampleProtos["NestedObject"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.NestedObject},
		FilesToGenerate:    []string{"NestedObject.proto"},
		ProtoFileName:      "NestedObject.proto",
	}

	// NoOneOf:
	sampleProtos["NoOneOf"] = SampleProto{
		AllowNullValues:    false,
		DisallowEnumOneOf:  true,
		DisallowOneOf:      true,
		ExpectedJsonSchema: []string{testdata.NoOneOf},
		FilesToGenerate:    []string{"NoOneOf.proto"},
		ProtoFileName:      "NoOneOf.proto",
	}

	// PayloadMessage:
	sampleProtos["PayloadMessage"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.PayloadMessage},
		FilesToGenerate:    []string{"PayloadMessage.proto"},
		ProtoFileName:      "PayloadMessage.proto",
	}

	// SeveralEnums:
	sampleProtos["SeveralEnums"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.FirstEnum, testdata.SecondEnum},
		FilesToGenerate:    []string{"SeveralEnums.proto"},
		ProtoFileName:      "SeveralEnums.proto",
	}

	// SeveralMessages:
	sampleProtos["SeveralMessages"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.FirstMessage, testdata.SecondMessage},
		FilesToGenerate:    []string{"SeveralMessages.proto"},
		ProtoFileName:      "SeveralMessages.proto",
	}

	// Timestamp
	sampleProtos["Timestamp"] = SampleProto{
		AllowNullValues:    false,
		ExpectedJsonSchema: []string{testdata.Timestamp},
		FilesToGenerate:    []string{"Timestamp.proto"},
		ProtoFileName:      "Timestamp.proto",
	}
}

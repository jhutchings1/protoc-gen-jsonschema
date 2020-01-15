package testdata

const ImportedMessageFromASiblingPackageWithEnum = `{
    "$schema": "http://json-schema.org/draft-04/schema#",
    "properties": {
        "msg": {
            "properties": {
                "repeat": {
                    "items": {
                        "enum": [
                            "FIZZ",
                            "BUZZ"
                        ],
                        "type": "string"
                    },
                    "type": "array"
                },
                "single": {
                    "enum": [
                        "FIZZ",
                        "BUZZ"
                    ],
                    "type": "string"
                }
            },
            "additionalProperties": true,
            "type": "object"
        }
    },
    "additionalProperties": true,
    "type": "object"
}`

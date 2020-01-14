package testdata

const ExternalEnum = `{
    "$schema": "http://json-schema.org/draft-04/schema#",
    "properties": {
        "single": {
            "enum": [
                "FOO",
                "BAR"
            ],
            "type": "string"
        },
        "stuff": {
            "items": {
                "enum": [
                    "FOO",
                    "BAR"
                ],
                "type": "string"
            },
            "type": "array"
        }
    },
    "additionalProperties": true,
    "type": "object"
}`

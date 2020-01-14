package testdata

const ExternalEnum = `{
    "$schema": "http://json-schema.org/draft-04/schema#",
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
}`

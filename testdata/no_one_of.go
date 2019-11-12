package testdata

const NoOneOf = `{
    "$schema": "http://json-schema.org/draft-04/schema#",
    "properties": {
        "bigNumber": {
            "type": "integer"
        },
        "someChoice": {
            "enum": [
                "FOO",
                "BAR",
                "FIZZ",
                "BUZZ"
            ],
            "type": "string"
        }
    },
    "additionalProperties": true,
    "type": "object"
}`

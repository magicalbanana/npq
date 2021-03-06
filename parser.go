package npq

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// Parser ...
type Parser interface {
	GetParsedQuery() string
	GetParsedParameters() []interface{}
	SetValue(parameterName string, parameterValue interface{})
	SetValuesFromMap(parameters map[string]interface{})
	SetValuesFromStruct(parameters interface{}) error
}

// parser handles the translation of named parameters to positional parameters, for SQL statements.
type parser struct {

	// A map of parameter names as keys, with value as a slice of positional indices which match
	// that parameter.
	positions map[string][]int

	// Contains all positional parameters, in order, ready to be used in the positional query.
	parameters []interface{}

	// The query containing named parameters, as passed in by Newparser
	originalQuery string

	// The query containing positional parameters, as generated by setQuery
	revisedQuery string
}

// NewParser creates a new named parameter query using the given
// queryText as a SQL query which contains named parameters. Named
// parameters are identified by starting with a ":" e.g., ":name" refers to
// the parameter "name", and ":foo" refers to the parameter "foo".
//
// Except for their names, named parameters follow all the same rules as
// positional parameters; they cannot be inside quoted strings, and cannot
// inject statements into a query. They can only be used to insert values.
func NewParser(queryText string) Parser {

	// TODO: I don't like using a map for such a small amount of elements.
	// If p becomes a bottleneck for anyone, the first thing to do would
	// be to make a slice and search routine for parameter positions.
	p := &parser{}
	p.positions = make(map[string][]int, 8)
	p.setQuery(queryText)

	return p
}

// setQuery parses out all named parameters, stores their locations, and
// builds a "revised" query which uses positional parameters.
func (p *parser) setQuery(queryText string) {

	var revisedBuilder bytes.Buffer
	var parameterBuilder bytes.Buffer
	var position []int
	var character rune
	var parameterName string
	var width int
	var positionIndex int

	p.originalQuery = queryText
	positionIndex = 0

	for i := 0; i < len(queryText); {

		character, width = utf8.DecodeRuneInString(queryText[i:])
		i += width

		// if it's a colon, do not write to builder, but grab name
		if character == ':' {

			for {

				character, width = utf8.DecodeRuneInString(queryText[i:])
				i += width

				if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' {
					parameterBuilder.WriteString(string(character))
				} else {
					break
				}
			}

			// add to positions
			parameterName = parameterBuilder.String()
			position = p.positions[parameterName]
			p.positions[parameterName] = append(position, positionIndex)
			positionIndex++

			// TODO: Add support for other drivers
			// Postgres placeholder syntax
			revisedBuilder.WriteString("$" + strconv.Itoa(positionIndex))
			parameterBuilder.Reset()

			if width <= 0 {
				break
			}
		}

		// otherwise write.
		revisedBuilder.WriteString(string(character))

		// if it's a quote, continue writing to builder, but do not search for parameters.
		if character == '\'' {

			for {

				character, width = utf8.DecodeRuneInString(queryText[i:])
				i += width
				revisedBuilder.WriteString(string(character))

				if character == '\'' {
					break
				}
			}
		}
	}

	p.revisedQuery = revisedBuilder.String()
	p.parameters = make([]interface{}, positionIndex)
}

// GetParsedQuery returns a version of the original query text
// whose named parameters have been replaced by positional parameters.
func (p *parser) GetParsedQuery() string {
	return p.revisedQuery
}

// GetParsedParameters returns an array of parameter objects that match the
// positional parameter list from GetParsedQuery
func (p *parser) GetParsedParameters() []interface{} {
	return p.parameters
}

// SetValue sets the value of the given [parameterName] to the given [parameterValue].
// If the parsed query does not have a placeholder for the given [parameterName],
// p method does nothing.
func (p *parser) SetValue(parameterName string, parameterValue interface{}) {

	for _, position := range p.positions[parameterName] {
		p.parameters[position] = parameterValue
	}
}

// SetValuesFromMap uses every key/value pair in the given [parameters] as a
// parameter replacement for p query. This is equivalent to calling SetValue
// for every key/value pair in the given [parameters] map.  If there are any
// keys/values present in the map that aren't part of the query, they are
// ignored.
func (p *parser) SetValuesFromMap(parameters map[string]interface{}) {

	for name, value := range parameters {
		p.SetValue(name, value)
	}
}

// SetValuesFromStruct uses reflection to find every public field of the given struct [parameters]
// and set their key/value as named parameters in p query.
// If the given [parameters] is not a struct, p will return an error.
//
// If you do not wish for a field in the struct to be added by its literal name,
// The struct may optionally specify the sqlParameterName as a tag on the field.
// e.g., a struct field may say something like:
//
// 	type Test struct {
// 		Foo string `sqlParameterName:"foobar"`
// 	}
func (p *parser) SetValuesFromStruct(parameters interface{}) error {

	var fieldValues reflect.Value
	var fieldValue reflect.Value
	var parameterType reflect.Type
	var parameterField reflect.StructField
	var queryTag string
	var visibilityCharacter rune

	fieldValues = reflect.ValueOf(parameters)

	if fieldValues.Kind() != reflect.Struct {
		return errors.New("Unable to add query values from parameter: parameter is not a struct")
	}

	parameterType = fieldValues.Type()

	for i := 0; i < fieldValues.NumField(); i++ {

		fieldValue = fieldValues.Field(i)
		parameterField = parameterType.Field(i)

		// public field?
		visibilityCharacter, _ = utf8.DecodeRuneInString(parameterField.Name[0:])

		if fieldValue.CanSet() || unicode.IsUpper(visibilityCharacter) {

			// check to see if p has a tag indicating a different query name
			queryTag = parameterField.Tag.Get("sqlParameterName")

			// otherwise just add the struct's name.
			if len(queryTag) <= 0 {
				queryTag = parameterField.Name
			}

			p.SetValue(queryTag, fieldValue.Interface())
		}
	}
	return nil
}

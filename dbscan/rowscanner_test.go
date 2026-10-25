package dbscan_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/georgysavva/scany/v2/dbscan"
)

type FooNested struct {
	FooNested string
}

type BarNested struct {
	BarNested string
}

type JSONObj struct {
	Key string
}

type NestedLevel1 struct {
	NestedLevel2
	Nested2 NestedLevel2
}

type NestedLevel2 struct {
	NestedLevel3
	Foo string
}

type NestedLevel3 struct {
	NestedLevel4
}

type NestedLevel4 struct {
	DeepNested1 string
	DeepNested2 string
}

type NestedWithTagLevel1 struct {
	NestedWithTagLevel2 `db:"nested2_tag_embedded"`
	Nested2Tag          NestedWithTagLevel2 `db:"nested2_tag"`
}

type NestedWithTagLevel2 struct {
	Foo string `db:"foo_column"`
}

type AmbiguousNested1 struct {
	Foo string
}

type AmbiguousNested2 struct {
	Foo string
}

type CustomScannableType struct {
	Key1 string
	Key2 string
}

func (cst *CustomScannableType) Scan(val interface{}) error {
	switch v := val.(type) {
	case []byte:
		return json.Unmarshal(v, cst)
	case string:
		return json.Unmarshal([]byte(v), cst)
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
}

func TestRowScanner_Scan_structDestination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		query    string
		expected interface{}
	}{
		{
			name: "fields without tag are filled from column via snake case mapping",
			query: `
				SELECT 'foo val' AS foo_column, 'bar val' AS bar_column
			`,
			expected: struct {
				FooColumn string
				BarColumn string
			}{
				FooColumn: "foo val",
				BarColumn: "bar val",
			},
		},
		{
			name: "fields with tag are filled from columns via tag",
			query: `
				SELECT 'foo val' AS foo_column, 'bar val' AS bar_column
			`,
			expected: struct {
				Foo string `db:"foo_column"`
				Bar string `db:"bar_column"`
			}{
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "fields with tag are filled from columns via tag that has multiple comma delimited values",
			query: `
				SELECT 'foo val' AS foo_column, 'bar val' AS bar_column
			`,
			expected: struct {
				Foo string `db:"foo_column,other_tag_value"`
				Bar string `db:"bar_column"`
			}{
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "string field by ptr",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar
			`,
			expected: struct {
				Foo *string
				Bar string
			}{
				Foo: makeStrPtr("foo val"),
				Bar: "bar val",
			},
		},
		{
			name: "string field by ptr NULL value",
			query: `
				SELECT NULL AS foo, 'bar val' AS bar
			`,
			expected: struct {
				Foo *string
				Bar string
			}{
				Foo: nil,
				Bar: "bar val",
			},
		},
		{
			name: "embedded structs are filled from columns without prefix",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as foo_nested, 'bar nested val' as bar_nested
			`,
			expected: struct {
				FooNested
				BarNested
				Foo string
				Bar string
			}{
				FooNested: FooNested{
					FooNested: "foo nested val",
				},
				BarNested: BarNested{
					BarNested: "bar nested val",
				},
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "nested structs without tag are filled from columns with snake case prefix",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as "foo_nested.foo_nested", 'bar nested val' as "bar_nested.bar_nested"
			`,
			expected: struct {
				FooNested FooNested
				BarNested BarNested
				Foo       string
				Bar       string
			}{
				FooNested: FooNested{
					FooNested: "foo nested val",
				},
				BarNested: BarNested{
					BarNested: "bar nested val",
				},
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "embedded struct with tag is filled from columns with prefix from the tag",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as "foo_nested.foo_nested"
			`,
			expected: struct {
				FooNested `db:"foo_nested"`
				Foo       string
				Bar       string
			}{
				FooNested: FooNested{
					FooNested: "foo nested val",
				},
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "nested struct with tag is filled from columns with prefix from the tag",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as "foo_nested_prefix.foo_nested"
			`,
			expected: struct {
				FooNested FooNested `db:"foo_nested_prefix"`
				Foo       string
				Bar       string
			}{
				FooNested: FooNested{
					FooNested: "foo nested val",
				},
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "nested struct with empty tag is filled from columns without prefix",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as "foo_nested"
			`,
			expected: struct {
				FooNested FooNested `db:""`
				Foo       string
				Bar       string
			}{
				FooNested: FooNested{
					FooNested: "foo nested val",
				},
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "embedded struct is unexported",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as foo_nested, 'bar nested val' as bar_nested
			`,
			expected: struct {
				nestedUnexported
				Foo string
				Bar string
			}{
				nestedUnexported: nestedUnexported{
					FooNested: "foo nested val",
					BarNested: "bar nested val",
				},
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "nested struct is unexported",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as "nested.foo_nested", 'bar nested val' as "nested.bar_nested"
			`,
			expected: struct {
				Nested nestedUnexported
				Foo    string
				Bar    string
			}{
				Nested: nestedUnexported{
					FooNested: "foo nested val",
					BarNested: "bar nested val",
				},
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "multiple level nested structs",
			query: `
				SELECT 'foo val 1' AS "foo", 'foo val 2' AS "nested2.foo", 
				'foo val 3' AS "nested1_tag_embedded.nested2_tag_embedded.foo_column",
				'foo val 4' AS "nested1_tag_embedded.nested2_tag.foo_column",
				'foo val 5' AS "nested1.foo", 'foo val 6' AS "nested1.nested2.foo", 
				'foo val 7' AS "nested1_tag.nested2_tag_embedded.foo_column",
				'foo val 8' AS "nested1_tag.nested2_tag.foo_column"
			`,
			expected: struct {
				NestedLevel1
				NestedWithTagLevel1 `db:"nested1_tag_embedded"`
				Nested1             NestedLevel1
				Nested1Tag          NestedWithTagLevel1 `db:"nested1_tag"`
			}{
				NestedLevel1: NestedLevel1{
					NestedLevel2: NestedLevel2{Foo: "foo val 1"},
					Nested2:      NestedLevel2{Foo: "foo val 2"},
				},
				NestedWithTagLevel1: NestedWithTagLevel1{
					NestedWithTagLevel2: NestedWithTagLevel2{Foo: "foo val 3"},
					Nested2Tag:          NestedWithTagLevel2{Foo: "foo val 4"},
				},
				Nested1: NestedLevel1{
					NestedLevel2: NestedLevel2{Foo: "foo val 5"},
					Nested2:      NestedLevel2{Foo: "foo val 6"},
				},
				Nested1Tag: NestedWithTagLevel1{
					NestedWithTagLevel2: NestedWithTagLevel2{Foo: "foo val 7"},
					Nested2Tag:          NestedWithTagLevel2{Foo: "foo val 8"},
				},
			},
		},
		{
			name: "nested structs by ptr are initialized and filled",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as foo_nested, 'bar nested val' as "bar_nested.bar_nested"
			`,
			expected: struct {
				*FooNested
				BarNested *BarNested
				Foo       string
				Bar       string
			}{
				FooNested: &FooNested{
					FooNested: "foo nested val",
				},
				BarNested: &BarNested{
					BarNested: "bar nested val",
				},
				Foo: "foo val",
				Bar: "bar val",
			},
		},
		{
			name: "nested structs by ptr are not initialized if not filled",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar
			`,
			expected: struct {
				*FooNested
				BarNested *BarNested
				Foo       string
				Bar       string
			}{
				FooNested: nil,
				BarNested: nil,
				Foo:       "foo val",
				Bar:       "bar val",
			},
		},
		{
			name: "ambiguous fields: scanned in the topmost field",
			query: `
				SELECT 'foo val' as foo
			`,
			expected: struct {
				AmbiguousNested1
				AmbiguousNested2
			}{
				AmbiguousNested1: AmbiguousNested1{Foo: "foo val"},
			},
		},
		{
			name: "ambiguous fields: scanned in the outermost field",
			query: `
				SELECT 'foo val' as foo
			`,
			expected: struct {
				AmbiguousNested1
				AmbiguousNested2
				Foo string
			}{
				Foo: "foo val",
			},
		},
		{
			name: "struct field is filled from a json column",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json, 'foo val' AS foo
			`,
			expected: struct {
				FooJSON JSONObj
				Foo     string
			}{
				FooJSON: JSONObj{Key: "key val"},
				Foo:     "foo val",
			},
		},
		{
			name: "struct field by ptr is filled from a json column",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json, 'foo val' AS foo
			`,
			expected: struct {
				FooJSON *JSONObj
				Foo     string
			}{
				FooJSON: &JSONObj{Key: "key val"},
				Foo:     "foo val",
			},
		},
		{
			name: "struct field by ptr is filled from a json column with NULL value",
			query: `
				SELECT NULL::JSON AS foo_json, 'foo val' AS foo
			`,
			expected: struct {
				FooJSON *JSONObj
				Foo     string
			}{
				FooJSON: nil,
				Foo:     "foo val",
			},
		},
		{
			name: "time field is filled from a timestamp column",
			query: `
				SELECT '2020-10-16 09:36:59+00:00'::timestamp AS foo
			`,
			expected: struct {
				Foo time.Time
			}{
				Foo: time.Date(2020, 10, 16, 9, 36, 59, 0, time.UTC),
			},
		},
		{
			name: "map field is filled from a json column",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json, 'foo val' AS foo
			`,
			expected: struct {
				FooJSON map[string]interface{}
				Foo     string
			}{
				FooJSON: map[string]interface{}{"key": "key val"},
				Foo:     "foo val",
			},
		},
		{
			name: "map field by ptr is filled from a json column",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json, 'foo val' AS foo
			`,
			expected: struct {
				FooJSON *map[string]interface{}
				Foo     string
			}{
				FooJSON: &map[string]interface{}{"key": "key val"},
				Foo:     "foo val",
			},
		},
		{
			name: "deeply nested structure is properly mapped",
			query: `
				SELECT 'deep_nested1 val' AS deep_nested1, 'deep_nested2 val' AS deep_nested2
			`,
			expected: NestedLevel1{
				NestedLevel2: NestedLevel2{
					NestedLevel3: NestedLevel3{
						NestedLevel4: NestedLevel4{
							DeepNested1: "deep_nested1 val",
							DeepNested2: "deep_nested2 val",
						},
					},
				},
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, tc.query)
			dst := allocateDestination(tc.expected)
			err := scan(t, dst, rows)
			require.NoError(t, err)
			assertDestinationEqual(t, tc.expected, dst)
		})
	}
}

type nestedUnexported struct {
	FooNested string
	BarNested string
}

func TestRowScanner_Scan_invalidStructDestination_returnsErr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		query       string
		dst         interface{}
		expectedErr string
	}{
		{
			name: "doesn't have a corresponding field",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar
			`,
			dst: &struct {
				Bar string
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'foo': no corresponding field found, or it's unexported in " +
				"struct { Bar string }",
		},
		{
			name: "the corresponding field is unexported",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar
			`,
			dst: &struct {
				foo string
				Bar string
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'foo': no corresponding field found, or it's unexported in " +
				"struct { foo string; Bar string }",
		},
		{
			name: "field with ignore tag isn't filled",
			query: `
				SELECT 'foo val' AS foo
			`,
			dst: &struct {
				Foo string `db:"-"`
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'foo': no corresponding field found, or it's unexported in " +
				"struct { Foo string \"db:\\\"-\\\"\" }",
		},
		{
			name: "nested struct field is unexported",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar,
					'foo nested val' as "foo_nested.foo_nested"
			`,
			dst: &struct {
				fooNested FooNested
				Foo       string
				Bar       string
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'foo_nested.foo_nested': no corresponding field found, or it's unexported in " +
				"struct { fooNested dbscan_test.FooNested; Foo string; Bar string }",
		},
		{
			name: "embedded struct with ignore tag isn't filled",
			query: `
				SELECT 'foo nested val' as "foo_nested" 
			`,
			dst: &struct {
				FooNested `db:"-"`
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'foo_nested': no corresponding field found, or it's unexported in " +
				"struct { dbscan_test.FooNested \"db:\\\"-\\\"\" }",
		},
		{
			name: "nested struct with ignore tag isn't filled",
			query: `
				SELECT 'foo nested val' as "foo_nested.foo_nested" 
			`,
			dst: &struct {
				FooNested FooNested `db:"-"`
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'foo_nested.foo_nested': no corresponding field found, or it's unexported in " +
				"struct { FooNested dbscan_test.FooNested \"db:\\\"-\\\"\" }",
		},
		{
			name: "field type does not match with column type",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar
			`,
			dst: &struct {
				Foo int
				Bar string
			}{},
			expectedErr: "doing scan: scanFn: scany: scan row into struct fields: can't scan into dest[0]: cannot scan text (OID 25) in text format into *int",
		},
		{
			name: "non struct embedded field",
			query: `
				SELECT 'foo val' AS foo, 'text' AS string
			`,
			dst: &struct {
				string `db:"string"`
				Foo    string
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'string': no corresponding field found, " +
				"or it's unexported in struct { string \"db:\\\"string\\\"\"; Foo string }",
		},
		{
			name: "embedded struct as destination field",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json, 'foo val' AS foo
			`,
			dst: &struct {
				JSONObj `db:"foo_json"`
				Foo     string
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'foo_json': no corresponding field found, " +
				"or it's unexported in struct { dbscan_test.JSONObj \"db:\\\"foo_json\\\"\"; Foo string }",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, tc.query)
			err := scan(t, tc.dst, rows)
			assert.EqualError(t, err, tc.expectedErr)
		})
	}
}

type Post struct {
	ID     string
	Author *Author
}

type Author struct {
	ID   string
	Name string
	Bio  *string
}

type AuthorWithProfile struct {
	ID      *string
	Profile *Profile
}

type Profile struct {
	Nickname string
	Avatar   *string
}

type AuthorWithAddress struct {
	ID      *string
	Address Address
}

type Address struct {
	City string
}

type AuthorDetails struct {
	ID          string
	CreatedAt   time.Time
	UpdatedAt   *time.Time
	Settings    JSONObj
	Meta        *JSONObj
	Tags        []string
	Attrs       map[string]interface{}
	Extra       interface{}
	Note        pgtype.Text
	Custom      CustomScannableType
	CustomByPtr *CustomScannableType
	Data        []byte
}

func TestRowScanner_Scan_withNilStructPointers(t *testing.T) {
	t.Parallel()
	api, err := getAPI(dbscan.WithNilStructPointers(true))
	require.NoError(t, err)
	cases := []struct {
		name     string
		query    string
		expected interface{}
	}{
		{
			name: "struct by ptr is nil if all its columns are NULL",
			query: `
				SELECT 'post val' AS id,
					NULL::TEXT AS "author.id", NULL::TEXT AS "author.name", NULL::TEXT AS "author.bio"
			`,
			expected: Post{ID: "post val", Author: nil},
		},
		{
			name: "struct by ptr is filled if its columns aren't NULL",
			query: `
				SELECT 'post val' AS id,
					'author id' AS "author.id", 'author name' AS "author.name", 'author bio' AS "author.bio"
			`,
			expected: Post{
				ID:     "post val",
				Author: &Author{ID: "author id", Name: "author name", Bio: makeStrPtr("author bio")},
			},
		},
		{
			name: "struct by ptr is filled if some of its columns aren't NULL",
			query: `
				SELECT 'post val' AS id,
					'author id' AS "author.id", 'author name' AS "author.name", NULL::TEXT AS "author.bio"
			`,
			expected: Post{
				ID:     "post val",
				Author: &Author{ID: "author id", Name: "author name", Bio: nil},
			},
		},
		{
			name: "struct by ptr is filled from columns of different types",
			query: `
				SELECT 'author id' AS "author.id",
					'2020-10-16 09:36:59+00:00'::TIMESTAMP AS "author.created_at",
					'2020-10-17 09:36:59+00:00'::TIMESTAMP AS "author.updated_at",
					'{"key": "settings val"}'::JSON AS "author.settings",
					'{"key": "meta val"}'::JSON AS "author.meta",
					ARRAY('tag val 1', 'tag val 2') AS "author.tags",
					'{"key": "attrs val"}'::JSON AS "author.attrs",
					'extra val' AS "author.extra",
					'note val' AS "author.note",
					'{"key1": "custom val 1", "key2": "custom val 2"}'::JSON AS "author.custom",
					'{"key1": "custom val 3", "key2": "custom val 4"}'::JSON AS "author.custom_by_ptr",
					'data val'::BYTEA AS "author.data"
			`,
			expected: struct {
				Author *AuthorDetails
			}{
				Author: &AuthorDetails{
					ID:          "author id",
					CreatedAt:   time.Date(2020, 10, 16, 9, 36, 59, 0, time.UTC),
					UpdatedAt:   makeTimePtr(time.Date(2020, 10, 17, 9, 36, 59, 0, time.UTC)),
					Settings:    JSONObj{Key: "settings val"},
					Meta:        &JSONObj{Key: "meta val"},
					Tags:        []string{"tag val 1", "tag val 2"},
					Attrs:       map[string]interface{}{"key": "attrs val"},
					Extra:       "extra val",
					Note:        pgtype.Text{String: "note val", Valid: true},
					Custom:      CustomScannableType{Key1: "custom val 1", Key2: "custom val 2"},
					CustomByPtr: &CustomScannableType{Key1: "custom val 3", Key2: "custom val 4"},
					Data:        []byte("data val"),
				},
			},
		},
		{
			name: "NULL columns are scanned into fields of non-nil struct by ptr that can hold NULL",
			query: `
				SELECT 'author id' AS "author.id",
					'2020-10-16 09:36:59+00:00'::TIMESTAMP AS "author.created_at",
					NULL::TIMESTAMP AS "author.updated_at",
					'{"key": "settings val"}'::JSON AS "author.settings",
					NULL::JSON AS "author.meta",
					NULL::TEXT[] AS "author.tags",
					NULL::JSON AS "author.attrs",
					NULL::TEXT AS "author.extra",
					NULL::TEXT AS "author.note",
					'{"key1": "custom val 1", "key2": "custom val 2"}'::JSON AS "author.custom",
					NULL::JSON AS "author.custom_by_ptr",
					NULL::BYTEA AS "author.data"
			`,
			expected: struct {
				Author *AuthorDetails
			}{
				Author: &AuthorDetails{
					ID:          "author id",
					CreatedAt:   time.Date(2020, 10, 16, 9, 36, 59, 0, time.UTC),
					UpdatedAt:   nil,
					Settings:    JSONObj{Key: "settings val"},
					Meta:        nil,
					Tags:        nil,
					Attrs:       nil,
					Extra:       nil,
					Note:        pgtype.Text{},
					Custom:      CustomScannableType{Key1: "custom val 1", Key2: "custom val 2"},
					CustomByPtr: nil,
					Data:        nil,
				},
			},
		},
		{
			name: "sibling structs by ptr are set independently",
			query: `
				SELECT 'author id' AS "author.id", 'author name' AS "author.name", NULL::TEXT AS "author.bio",
					NULL::TEXT AS "editor.id", NULL::TEXT AS "editor.name", NULL::TEXT AS "editor.bio"
			`,
			expected: struct {
				Author *Author
				Editor *Author
			}{
				Author: &Author{ID: "author id", Name: "author name"},
				Editor: nil,
			},
		},
		{
			name: "nested struct by ptr is nil if all its columns are NULL",
			query: `
				SELECT 'author id' AS "author.id",
					NULL::TEXT AS "author.profile.nickname", NULL::TEXT AS "author.profile.avatar"
			`,
			expected: struct {
				Author *AuthorWithProfile
			}{
				Author: &AuthorWithProfile{ID: makeStrPtr("author id"), Profile: nil},
			},
		},
		{
			name: "parent struct by ptr is nil if columns of its nested struct by ptr are NULL too",
			query: `
				SELECT NULL::TEXT AS "author.id",
					NULL::TEXT AS "author.profile.nickname", NULL::TEXT AS "author.profile.avatar"
			`,
			expected: struct {
				Author *AuthorWithProfile
			}{
				Author: nil,
			},
		},
		{
			name: "parent struct by ptr is filled if a column of its nested struct by ptr isn't NULL",
			query: `
				SELECT NULL::TEXT AS "author.id",
					'nickname val' AS "author.profile.nickname", NULL::TEXT AS "author.profile.avatar"
			`,
			expected: struct {
				Author *AuthorWithProfile
			}{
				Author: &AuthorWithProfile{ID: nil, Profile: &Profile{Nickname: "nickname val"}},
			},
		},
		{
			name: "columns of struct by value count as columns of its parent struct by ptr",
			query: `
				SELECT NULL::TEXT AS "author.id", NULL::TEXT AS "author.address.city"
			`,
			expected: struct {
				Author *AuthorWithAddress
			}{
				Author: nil,
			},
		},
		{
			name: "struct by value is filled even if all its columns are NULL",
			query: `
				SELECT NULL::TEXT AS "author.id",
					NULL::TEXT AS "author.profile.nickname", NULL::TEXT AS "author.profile.avatar"
			`,
			expected: struct {
				Author AuthorWithProfile
			}{
				Author: AuthorWithProfile{ID: nil, Profile: nil},
			},
		},
		{
			name: "embedded struct by ptr is nil if all its columns are NULL",
			query: `
				SELECT 'post val' AS post_id, NULL::TEXT AS id, NULL::TEXT AS name, NULL::TEXT AS bio,
					NULL::TEXT AS "editor.id", NULL::TIMESTAMP AS "editor.updated_at"
			`,
			expected: struct {
				PostID string
				*Author
				*AuthorDetails `db:"editor"`
			}{
				PostID: "post val",
			},
		},
		{
			name: "embedded struct by ptr is filled if its columns aren't NULL",
			query: `
				SELECT 'post val' AS post_id, 'author id' AS id, 'author name' AS name, NULL::TEXT AS bio
			`,
			expected: struct {
				PostID string
				*Author
			}{
				PostID: "post val",
				Author: &Author{ID: "author id", Name: "author name"},
			},
		},
		{
			name: "field by ptr to struct that is scanned from a single column is nil if it's NULL",
			query: `
				SELECT 'post val' AS id, NULL::JSON AS foo_json
			`,
			expected: struct {
				ID      string
				FooJSON *JSONObj
			}{
				ID:      "post val",
				FooJSON: nil,
			},
		},
		{
			name: "NULL column mapped to struct by ptr itself makes it nil despite columns of its fields",
			query: `
				SELECT 'author id' AS "author.id", NULL::JSON AS "author.meta", 'key val' AS "author.meta.key"
			`,
			expected: struct {
				Author *struct {
					ID   string
					Meta *JSONObj
				}
			}{
				Author: &struct {
					ID   string
					Meta *JSONObj
				}{ID: "author id", Meta: nil},
			},
		},
		{
			name: "struct by ptr is nil if rows don't contain its columns",
			query: `
				SELECT 'post val' AS id
			`,
			expected: Post{ID: "post val", Author: nil},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, tc.query)
			dst := allocateDestination(tc.expected)
			err := scanWithAPI(t, api, dst, rows)
			require.NoError(t, err)
			assertDestinationEqual(t, tc.expected, dst)
		})
	}
}

func TestRowScanner_Scan_withNilStructPointers_returnsErr(t *testing.T) {
	t.Parallel()
	api, err := getAPI(dbscan.WithNilStructPointers(true))
	require.NoError(t, err)
	cases := []struct {
		name        string
		query       string
		dst         interface{}
		expectedErr string
	}{
		{
			name: "NULL column of non-nil struct by ptr is scanned into string field",
			query: `
				SELECT 'post val' AS id,
					'author id' AS "author.id", NULL::TEXT AS "author.name", 'author bio' AS "author.bio"
			`,
			dst: &Post{},
			expectedErr: "doing scan: scanFn: scany: column: 'author.name': cannot scan NULL into string: " +
				"*dbscan_test.Author struct isn't nil because not all of its columns are NULL",
		},
		{
			name: "NULL column of non-nil struct by ptr is scanned into time field",
			query: `
				SELECT 'author id' AS "author.id", NULL::TIMESTAMP AS "author.created_at"
			`,
			dst: &struct {
				Author *AuthorDetails
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'author.created_at': cannot scan NULL into time.Time: " +
				"*dbscan_test.AuthorDetails struct isn't nil because not all of its columns are NULL",
		},
		{
			name: "NULL column of non-nil nested struct by ptr is scanned into string field",
			query: `
				SELECT 'author id' AS "author.id",
					NULL::TEXT AS "author.profile.nickname", 'avatar val' AS "author.profile.avatar"
			`,
			dst: &struct {
				Author *AuthorWithProfile
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'author.profile.nickname': cannot scan NULL into string: " +
				"*dbscan_test.Profile struct isn't nil because not all of its columns are NULL",
		},
		{
			name: "sql.Scanner returns an error for NULL column of non-nil struct by ptr",
			query: `
				SELECT 'author id' AS "author.id", NULL::JSON AS "author.custom"
			`,
			dst: &struct {
				Author *AuthorDetails
			}{},
			expectedErr: "doing scan: scanFn: scany: column: 'author.custom': " +
				"scan NULL into dbscan_test.CustomScannableType: unsupported type: <nil>",
		},
		{
			name: "column type does not match with field type of struct by ptr",
			query: `
				SELECT 'post val' AS id, 'author id' AS "author.id", 'author name' AS "author.name"
			`,
			dst: &struct {
				ID     string
				Author *struct {
					ID   string
					Name int
				}
			}{},
			expectedErr: "doing scan: scanFn: scany: scan row into struct fields: can't scan into dest[2]: " +
				"cannot scan text (OID 25) in text format into **int",
		},
		{
			name: "struct by ptr doesn't have a corresponding field",
			query: `
				SELECT 'author id' AS "author.id", 'author email' AS "author.email"
			`,
			dst: &Post{},
			expectedErr: "doing scan: scanFn: scany: column: 'author.email': no corresponding field found, " +
				"or it's unexported in dbscan_test.Post",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, tc.query)
			err := scanWithAPI(t, api, tc.dst, rows)
			assert.EqualError(t, err, tc.expectedErr)
		})
	}
}

func TestRowScanner_Scan_withNilStructPointers_sameDestination(t *testing.T) {
	t.Parallel()
	api, err := getAPI(dbscan.WithNilStructPointers(true))
	require.NoError(t, err)
	rows := queryRows(t, postsWithAuthorsQuery)
	defer rows.Close() //nolint: errcheck
	rs := api.NewRowScanner(rows)

	type scanned struct {
		id     string
		author *Author
	}
	var got []scanned
	var firstBio *string
	dst := &Post{}
	for rows.Next() {
		err := rs.Scan(dst)
		require.NoError(t, err)
		row := scanned{id: dst.ID}
		if dst.Author != nil {
			author := *dst.Author
			row.author = &author
			if firstBio == nil {
				firstBio = dst.Author.Bio
			}
		}
		got = append(got, row)
	}
	requireNoRowsErrorsAndClose(t, rows)

	expected := []scanned{
		{id: "post 1", author: &Author{ID: "author 1", Name: "name 1", Bio: makeStrPtr("bio 1")}},
		{id: "post 2", author: &Author{ID: "author 2", Name: "name 2", Bio: nil}},
		{id: "post 3", author: nil},
		{id: "post 4", author: &Author{ID: "author 1", Name: "name 1", Bio: makeStrPtr("bio 1")}},
	}
	assert.Equal(t, expected, got)
	// Bio of the first row isn't overwritten by the next rows.
	assert.Equal(t, makeStrPtr("bio 1"), firstBio)
	assert.NotSame(t, firstBio, dst.Author.Bio)
}

func TestRowScanner_Scan_withNilStructPointersAndAllowUnknownColumns(t *testing.T) {
	t.Parallel()
	api, err := getAPI(dbscan.WithNilStructPointers(true), dbscan.WithAllowUnknownColumns(true))
	require.NoError(t, err)
	rows := queryRows(t, `
		SELECT 'post val' AS id, NULL::TEXT AS "author.id", 'email val' AS "author.email",
			NULL::TEXT AS "author.name", NULL::TEXT AS "author.bio"
	`)
	expected := Post{ID: "post val", Author: nil}

	var got Post
	err = scanWithAPI(t, api, &got, rows)
	require.NoError(t, err)

	assert.Equal(t, expected, got)
}

func TestRowScanner_Scan_withoutNilStructPointers_initializesStructByPtr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		dst         interface{}
		expected    interface{}
		expectedErr string
	}{
		{
			name: "fields by ptr",
			dst: &struct {
				Author *AuthorWithProfile
			}{},
			expected: &struct {
				Author *AuthorWithProfile
			}{
				Author: &AuthorWithProfile{ID: nil, Profile: &Profile{Nickname: "", Avatar: nil}},
			},
		},
		{
			name: "fields by value",
			dst: &struct {
				Author *struct {
					ID      string
					Profile *Profile
				}
			}{},
			expectedErr: "doing scan: scanFn: scany: scan row into struct fields: can't scan into dest[0]: " +
				"cannot scan NULL into *string",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, `
				SELECT NULL::TEXT AS "author.id", NULL::TEXT AS "author.profile.avatar"
			`)
			err := scan(t, tc.dst, rows)
			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, tc.dst)
		})
	}
}

func TestRowScanner_Scan_mapDestination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		query    string
		expected interface{}
	}{
		{
			name: "map[string]interface{}",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar
			`,
			expected: map[string]interface{}{
				"foo": "foo val",
				"bar": "bar val",
			},
		},
		{
			name: "map[string]string{}",
			query: `
				SELECT 'foo val' AS foo, 'bar val' AS bar
			`,
			expected: map[string]string{
				"foo": "foo val",
				"bar": "bar val",
			},
		},
		{
			name: "map[string]*string{}",
			query: `
				SELECT 'foo val' AS foo, NULL AS bar
			`,
			expected: map[string]*string{
				"foo": makeStrPtr("foo val"),
				"bar": nil,
			},
		},
		{
			name: "map[string]struct{}",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json, '{"key": "key val 2"}'::JSON AS bar_json
			`,
			expected: map[string]JSONObj{
				"foo_json": {Key: "key val"},
				"bar_json": {Key: "key val 2"},
			},
		},
		{
			name: "map[string]*struct{}",
			query: `
        SELECT '{"key": "key val"}'::JSON AS foo_json, NULL::JSON AS bar_json
			`,
			expected: map[string]*JSONObj{
				"foo_json": {Key: "key val"},
				"bar_json": nil,
			},
		},
		{
			name: "map[string]map[string]interface{}",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json, '{"key": "key val 2"}'::JSON AS bar_json
			`,
			expected: map[string]map[string]interface{}{
				"foo_json": {"key": "key val"},
				"bar_json": {"key": "key val 2"},
			},
		},
		{
			name: "map[string]*map[string]interface{}",
			query: `
        SELECT '{"key": "key val"}'::JSON AS foo_json, NULL::JSON AS bar_json
			`,
			expected: map[string]*map[string]interface{}{
				"foo_json": {"key": "key val"},
				"bar_json": nil,
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, tc.query)
			dst := allocateDestination(tc.expected)
			err := scan(t, dst, rows)
			require.NoError(t, err)
			assertDestinationEqual(t, tc.expected, dst)
		})
	}
}

func TestRowScanner_Scan_invalidMapDestination_returnsErr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		query       string
		dst         interface{}
		expectedErr string
	}{
		{
			name:        "non string key",
			query:       singleRowsQuery,
			dst:         &map[int]interface{}{},
			expectedErr: "doing scan: starting: scany: invalid type map[int]interface {}: map must have string key, got: int",
		},
		{
			name: "value type does not match with column type",
			query: `
				SELECT 'foo val' AS foo
			`,
			dst:         &map[string]int{},
			expectedErr: "doing scan: scanFn: scany: scan rows into map: can't scan into dest[0]: cannot scan text (OID 25) in text format into *int",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, tc.query)
			err := scan(t, tc.dst, rows)
			assert.EqualError(t, err, tc.expectedErr)
		})
	}
}

func TestRowScanner_Scan_primitiveTypeDestination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		query    string
		expected interface{}
	}{
		{
			name: "string",
			query: `
				SELECT 'foo val' AS foo 
			`,
			expected: "foo val",
		},
		{
			name: "string by ptr",
			query: `
				SELECT 'foo val' AS foo 
			`,
			expected: makeStrPtr("foo val"),
		},
		{
			name: "string by ptr NULL value",
			query: `
				SELECT NULL AS foo 
			`,
			expected: (*string)(nil),
		},
		{
			name: "slice",
			query: `
				SELECT ARRAY('foo val', 'foo val 2', 'foo val 3') AS foo 
			`,
			expected: []string{"foo val", "foo val 2", "foo val 3"},
		},
		{
			name: "slice by ptr",
			query: `
				SELECT ARRAY('foo val', 'foo val 2', 'foo val 3') AS foo 
			`,
			expected: &[]string{"foo val", "foo val 2", "foo val 3"},
		},
		{
			name: "struct by ptr treated as primitive type",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json
			`,

			expected: &JSONObj{Key: "key val"},
		},
		{
			name: "map by ptr treated as primitive type",
			query: `
				SELECT '{"key": "key val"}'::JSON AS foo_json
			`,
			expected: &map[string]interface{}{"key": "key val"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, tc.query)
			dst := allocateDestination(tc.expected)
			err := scan(t, dst, rows)
			require.NoError(t, err)
			assertDestinationEqual(t, tc.expected, dst)
		})
	}
}

func TestRowScanner_Scan_primitiveTypeDestinationDoesNotMatchWithColumnType_returnsErr(t *testing.T) {
	t.Parallel()
	query := `
		SELECT 'foo val' AS foo
	`
	rows := queryRows(t, query)
	expectedErr := "doing scan: scanFn: scany: scan row value into a primitive type: can't scan into dest[0]: cannot scan text (OID 25) in text format into *int"
	dst := new(int)
	err := scan(t, dst, rows)
	assert.EqualError(t, err, expectedErr)
}

func TestRowScanner_Scan_primitiveTypeDestinationRowsContainMoreThanOneColumn_returnsErr(t *testing.T) {
	t.Parallel()
	query := `
		SELECT '1 val' AS column1, '2 val' AS column2
	`
	rows := queryRows(t, query)
	expectedErr := "doing scan: starting: scany: to scan into a primitive type, columns number must be exactly 1, got: 2"
	dst := new(string)
	err := scan(t, dst, rows)
	assert.EqualError(t, err, expectedErr)
}

// It seems that there is no way to select result set with 0 columns from crdb server.
// So this type exists in order to check that dbscan handles this cases properly.
type emptyRow struct{}

func (er emptyRow) Scan(_ ...interface{}) error { return nil }
func (er emptyRow) Next() bool                  { return true }
func (er emptyRow) Columns() ([]string, error)  { return []string{}, nil }
func (er emptyRow) Close() error                { return nil }
func (er emptyRow) Err() error                  { return nil }
func (er emptyRow) NextResultSet() bool         { return false }

func TestRowScanner_Scan_primitiveTypeDestinationRowsContainZeroColumns_returnsErr(t *testing.T) {
	t.Parallel()
	rows := emptyRow{}
	expectedErr := "doing scan: starting: scany: to scan into a primitive type, columns number must be exactly 1, got: 0"
	dst := new(string)
	err := scan(t, dst, rows)
	assert.EqualError(t, err, expectedErr)
}

func TestRowScanner_Scan_ScannableTypeDestination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		scannableTypes []interface{}
		query          string
		expected       interface{}
	}{
		{
			name: "pgtype.Text destination type",
			query: `
				SELECT 'foo val' AS foo 
			`,
			expected: pgtype.Text{String: "foo val", Valid: true},
		},
		{
			name: "CustomScannableType destination type",
			query: `
				SELECT '{"key1": "foo val", "key2": "bar val"}'::JSON AS foo 
			`,
			expected: CustomScannableType{Key1: "foo val", Key2: "bar val"},
		},
		{
			name: "*CustomScannableType destination type",
			query: `
				SELECT '{"key1": "foo val", "key2": "bar val"}'::JSON AS foo 
			`,
			expected: &CustomScannableType{Key1: "foo val", Key2: "bar val"},
		},
		{
			name: "*CustomScannableType destination type NULL value",
			query: `
				SELECT NULL::JSON AS foo 
			`,
			expected: (*CustomScannableType)(nil),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, tc.query)
			dst := allocateDestination(tc.expected)
			err := scan(t, dst, rows)
			require.NoError(t, err)
			assertDestinationEqual(t, tc.expected, dst)
		})
	}
}

func TestRowScanner_Scan_rowsContainDuplicateColumns_returnsErr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		dst  interface{}
	}{
		{
			name: "struct destination",
			dst: &struct {
				Foo string
			}{},
		},
		{
			name: "map destination",
			dst:  &map[string]interface{}{},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			query := `
				SELECT 'foo val' AS foo, 'foo val' AS foo
			`
			rows := queryRows(t, query)
			expectedErr := "doing scan: starting: duplicate columns: scany: rows contain a duplicate column 'foo'"
			err := scan(t, tc.dst, rows)
			assert.EqualError(t, err, expectedErr)
		})
	}
}

func TestRowScanner_Scan_invalidDst_returnsErr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		dst         interface{}
		expectedErr string
	}{
		{
			name: "non pointer",
			dst: struct {
				Foo string
			}{},
			expectedErr: "parsing destination: scany: destination must be a pointer, got: struct { Foo string }",
		},
		{
			name:        "map",
			dst:         map[string]interface{}{},
			expectedErr: "parsing destination: scany: destination must be a pointer, got: map[string]interface {}",
		},
		{
			name:        "slice",
			dst:         []struct{ Foo string }{},
			expectedErr: "parsing destination: scany: destination must be a pointer, got: []struct { Foo string }",
		},
		{
			name:        "nil",
			dst:         nil,
			expectedErr: "parsing destination: scany: destination must be a non nil pointer",
		},
		{
			name:        "(*int)(nil)",
			dst:         (*int)(nil),
			expectedErr: "parsing destination: scany: destination must be a non nil pointer",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := queryRows(t, `SELECT 1`)
			err := scan(t, tc.dst, rows)
			assert.EqualError(t, err, tc.expectedErr)
		})
	}
}

func TestRowScanner_Scan_startCalledExactlyOnce(t *testing.T) {
	t.Parallel()
	dbscan.DoTestRowScannerStartCalledExactlyOnce(t, testAPI, queryRows)
}

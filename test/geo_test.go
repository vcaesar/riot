//  Copyright (c) 2020 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 		http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	bluge "github.com/vcaesar/riot"
	"github.com/vcaesar/riot/numeric/geo"
	"github.com/vcaesar/riot/search"
)

func geoLoad(writer *bluge.Writer) error {
	err := writer.Insert(bluge.NewDocument("amoeba_brewery").
		AddField(bluge.NewKeywordField("name", "amoeba brewery")).
		AddField(bluge.NewGeoPointField("geo", 77.60490, 12.97467)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("brewpub_on_the_green").
		AddField(bluge.NewKeywordField("name", "Brewpub-on-the-Green")).
		AddField(bluge.NewGeoPointField("geo", -121.989, 37.5483)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("capital_city_brewing_company").
		AddField(bluge.NewKeywordField("name", "Capital City Brewing Company")).
		AddField(bluge.NewGeoPointField("geo", -77.0272, 38.8999)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("communiti_brewery").
		AddField(bluge.NewKeywordField("name", "communiti brewery")).
		AddField(bluge.NewGeoPointField("geo", 77.608237, 12.97237)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("firehouse_grill_brewery").
		AddField(bluge.NewKeywordField("name", "Firehouse Grill & Brewery")).
		AddField(bluge.NewGeoPointField("geo", -122.03, 37.3775)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("hook_ladder_brewing_company").
		AddField(bluge.NewKeywordField("name", "Hook & Ladder Brewing Company")).
		AddField(bluge.NewGeoPointField("geo", -77.0237, 38.9911)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("jack_s_brewing").
		AddField(bluge.NewKeywordField("name", "Jack's Brewing")).
		AddField(bluge.NewGeoPointField("geo", -121.988, 37.5441)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("social_brewery").
		AddField(bluge.NewKeywordField("name", "social brewery")).
		AddField(bluge.NewGeoPointField("geo", 77.6042133, 12.9736946)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("sweet_water_tavern_and_brewery").
		AddField(bluge.NewKeywordField("name", "Sweet Water Tavern and Brewery")).
		AddField(bluge.NewGeoPointField("geo", -77.4097, 39.0324)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	return nil
}

func geoTests() []*RequestVerify {
	sortByDistanceFromCouchbaseOffice := search.SortBy(
		search.NewGeoPointDistanceSource(
			search.Field("geo"),
			search.NewConstantGeoPointSource(
				geo.Point{
					Lon: -122.107799, Lat: 37.399285,
				}),
			geo.Mile))

	return []*RequestVerify{
		{
			Comment: "breweries near the couchbase office",
			Request: bluge.NewTopNSearch(10,
				bluge.NewGeoDistanceQuery(-122.107799, 37.399285, "100mi").
					SetField("geo")).
				SortBy([]string{"_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  3,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("brewpub_on_the_green")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("firehouse_grill_brewery")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("jack_s_brewing")},
					},
				},
			},
		},
		{
			Comment: "breweries near the whitehouse",
			Request: bluge.NewTopNSearch(10,
				bluge.NewGeoDistanceQuery(-77.0365, 38.8977, "100mi").
					SetField("geo")).
				SortBy([]string{"_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  3,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("capital_city_brewing_company")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("hook_ladder_brewing_company")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("sweet_water_tavern_and_brewery")},
					},
				},
			},
		},
		{
			Comment: "bounding box of USA",
			Request: bluge.NewTopNSearch(10,
				bluge.NewGeoBoundingBoxQuery(-125.0011, 49.5904, -66.9326, 24.9493).
					SetField("geo")).
				SortBy([]string{"_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("brewpub_on_the_green")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("capital_city_brewing_company")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("firehouse_grill_brewery")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("hook_ladder_brewing_company")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("jack_s_brewing")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("sweet_water_tavern_and_brewery")},
					},
				},
			},
		},
		{
			Comment: "bounding box around DC area",
			Request: bluge.NewTopNSearch(10,
				bluge.NewGeoBoundingBoxQuery(-78, 39.5, -76, 38.5).
					SetField("geo")).
				SortBy([]string{"_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  3,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("capital_city_brewing_company")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("hook_ladder_brewing_company")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("sweet_water_tavern_and_brewery")},
					},
				},
			},
		},
		{
			Comment: "breweries near the couchbase office, ordered by distance from office",
			Request: bluge.NewTopNSearch(10,
				bluge.NewGeoDistanceQuery(-122.107799, 37.399285, "100mi").
					SetField("geo")).
				SortByCustom(search.SortOrder{
					sortByDistanceFromCouchbaseOffice,
				}),
			Aggregations: standardAggs,
			ExpectTotal:  3,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("firehouse_grill_brewery")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("jack_s_brewing")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("brewpub_on_the_green")},
					},
				},
			},
		},
		{
			Comment: "polygon around cb office area, using GeoJSON lat/lon as array",
			Request: bluge.NewTopNSearch(10,
				bluge.NewGeoBoundingPolygonQuery([]geo.Point{
					{
						Lon: 77.607749,
						Lat: 12.974872,
					},
					{
						Lon: 77.6101101,
						Lat: 12.971725,
					},
					{
						Lon: 77.606912,
						Lat: 12.972530,
					},
					{
						Lon: 77.603780,
						Lat: 12.975112,
					},
				}).
					SetField("geo")).
				SortBy([]string{"_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("amoeba_brewery")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("communiti_brewery")},
					},
				},
			},
		},
	}
}

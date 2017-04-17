package gocb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type viewResponse struct {
	TotalRows int               `json:"total_rows,omitempty"`
	Rows      []json.RawMessage `json:"rows,omitempty"`
	Error     string            `json:"error,omitempty"`
	Reason    string            `json:"reason,omitempty"`
}

type viewError struct {
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

func (e *viewError) Error() string {
	return e.Message + " - " + e.Reason
}

// ViewResults implements an iterator interface which can be used to iterate over the rows of the query results.
type ViewResults interface {
	One(valuePtr interface{}) error
	Next(valuePtr interface{}) bool
	NextBytes() []byte
	Close() error
}

// ViewResultMetrics allows access to the TotalRows value from the view response.  This is
// implemented as an additional interface to maintain ABI compatibility for the 1.x series.
type ViewResultMetrics interface {
	TotalRows() int
}

type viewResults struct {
	index     int
	rows      []json.RawMessage
	totalRows int
	err       error
}

func (r *viewResults) Next(valuePtr interface{}) bool {
	if r.err != nil {
		return false
	}

	row := r.NextBytes()
	if row == nil {
		return false
	}

	r.err = json.Unmarshal(row, valuePtr)
	if r.err != nil {
		return false
	}

	return true
}

func (r *viewResults) NextBytes() []byte {
	if r.err != nil {
		return nil
	}

	if r.index+1 >= len(r.rows) {
		return nil
	}
	r.index++

	return r.rows[r.index]
}

func (r *viewResults) Close() error {
	return r.err
}

func (r *viewResults) One(valuePtr interface{}) error {
	if !r.Next(valuePtr) {
		err := r.Close()
		if err != nil {
			return err
		}
		return ErrNoResults
	}

	// Ignore any errors occurring after we already have our result
	err := r.Close()
	if err != nil {
		// Return no error as we got the one result already.
		return nil
	}

	return nil
}

func (r *viewResults) TotalRows() int {
	return r.totalRows
}

func (b *Bucket) executeViewQuery(viewType, ddoc, viewName string, options url.Values) (ViewResults, error) {
	capiEp, err := b.getViewEp()
	if err != nil {
		return nil, err
	}

	reqUri := fmt.Sprintf("%s/_design/%s/%s/%s?%s", capiEp, ddoc, viewType, viewName, options.Encode())

	req, err := http.NewRequest("GET", reqUri, nil)
	if err != nil {
		return nil, err
	}

	if b.cluster.auth != nil {
		userPass := b.cluster.auth.bucketViews(b.name)
		req.SetBasicAuth(userPass.Username, userPass.Password)
	} else {
		req.SetBasicAuth(b.name, b.password)
	}

	resp, err := doHttpWithTimeout(b.client.HttpClient(), req, b.viewTimeout)
	if err != nil {
		return nil, err
	}

	viewResp := viewResponse{}
	jsonDec := json.NewDecoder(resp.Body)
	err = jsonDec.Decode(&viewResp)
	if err != nil {
		return nil, err
	}

	err = resp.Body.Close()
	if err != nil {
		logDebugf("Failed to close socket (%s)", err)
	}

	if resp.StatusCode != 200 {
		if viewResp.Error != "" {
			return nil, &viewError{
				Message: viewResp.Error,
				Reason:  viewResp.Reason,
			}
		}

		return nil, &viewError{
			Message: "HTTP Error",
			Reason:  fmt.Sprintf("Status code was %d.", resp.StatusCode),
		}
	}

	return &viewResults{
		index:     -1,
		rows:      viewResp.Rows,
		totalRows: viewResp.TotalRows,
	}, nil
}

// ExecuteViewQuery performs a view query and returns a list of rows or an error.
func (b *Bucket) ExecuteViewQuery(q *ViewQuery) (ViewResults, error) {
	ddoc, name, opts, err := q.getInfo()
	if err != nil {
		return nil, err
	}

	return b.executeViewQuery("_view", ddoc, name, opts)
}

// ExecuteSpatialQuery performs a spatial query and returns a list of rows or an error.
func (b *Bucket) ExecuteSpatialQuery(q *SpatialQuery) (ViewResults, error) {
	ddoc, name, opts, err := q.getInfo()
	if err != nil {
		return nil, err
	}

	return b.executeViewQuery("_spatial", ddoc, name, opts)
}

// ExecuteN1qlQuery performs a n1ql query and returns a list of rows or an error.
func (b *Bucket) ExecuteN1qlQuery(q *N1qlQuery, params interface{}) (QueryResults, error) {
	return b.cluster.doN1qlQuery(b, q, params)
}

// ExecuteSearchQuery performs a view query and returns a list of rows or an error.
//
// Experimental: This API is subject to change at any time.
func (b *Bucket) ExecuteSearchQuery(q *SearchQuery) (SearchResults, error) {
	return b.cluster.doSearchQuery(b, q)
}

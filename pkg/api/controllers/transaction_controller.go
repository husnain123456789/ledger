package controllers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/numary/go-libs/sharedapi"
	"github.com/numary/ledger/pkg/core"
	"github.com/numary/ledger/pkg/ledger"
	"github.com/numary/ledger/pkg/storage"
	"github.com/numary/ledger/pkg/storage/sqlstorage"
)

type TransactionController struct{}

func NewTransactionController() TransactionController {
	return TransactionController{}
}

func (ctl *TransactionController) CountTransactions(c *gin.Context) {
	l, _ := c.Get("ledger")

	count, err := l.(*ledger.Ledger).CountTransactions(
		c.Request.Context(),
		storage.SetReferenceFilter(c.Query("reference")),
		storage.SetAccountFilter(c.Query("account")),
		storage.SetSourceFilter(c.Query("source")),
		storage.SetDestinationFilter(c.Query("destination")),
	)
	if err != nil {
		ResponseError(c, err)
		return
	}

	c.Header("Count", fmt.Sprint(count))
}

func (ctl *TransactionController) GetTransactions(c *gin.Context) {
	l, _ := c.Get("ledger")

	var cursor sharedapi.Cursor[core.Transaction]
	var err error

	if c.Query("pagination_token") != "" {
		if c.Query("after") != "" || c.Query("reference") != "" ||
			c.Query("account") != "" || c.Query("source") != "" ||
			c.Query("destination") != "" || c.Query("start_time") != "" ||
			c.Query("end_time") != "" {
			ResponseError(c, ledger.NewValidationError(
				"no other query params can be set with 'pagination_token'"))
			return
		}

		res, decErr := base64.RawURLEncoding.DecodeString(c.Query("pagination_token"))
		if decErr != nil {
			ResponseError(c, ledger.NewValidationError("invalid query value 'pagination_token'"))
			return
		}

		token := sqlstorage.TxsPaginationToken{}
		if err = json.Unmarshal(res, &token); err != nil {
			ResponseError(c, ledger.NewValidationError("invalid query value 'pagination_token'"))
			return
		}

		cursor, err = l.(*ledger.Ledger).GetTransactions(c.Request.Context(),
			storage.SetAfterTxID(token.AfterTxID),
			storage.SetReferenceFilter(token.ReferenceFilter),
			storage.SetAccountFilter(token.AccountFilter),
			storage.SetSourceFilter(token.SourceFilter),
			storage.SetDestinationFilter(token.DestinationFilter),
			storage.SetStartTime(token.StartTime),
			storage.SetEndTime(token.EndTime),
		)
	} else {
		var afterTxIDParsed uint64
		if c.Query("after") != "" {
			afterTxIDParsed, err = strconv.ParseUint(c.Query("after"), 10, 64)
			if err != nil {
				ResponseError(c, ledger.NewValidationError("invalid query value 'after'"))
				return
			}
		}

		var startTimeParsed, endTimeParsed time.Time
		if c.Query("start_time") != "" {
			startTimeParsed, err = time.Parse(time.RFC3339, c.Query("start_time"))
			if err != nil {
				ResponseError(c, ledger.NewValidationError("invalid query value 'start_time'"))
				return
			}
		}

		if c.Query("end_time") != "" {
			endTimeParsed, err = time.Parse(time.RFC3339, c.Query("end_time"))
			if err != nil {
				ResponseError(c, ledger.NewValidationError("invalid query value 'end_time'"))
				return
			}
		}

		cursor, err = l.(*ledger.Ledger).GetTransactions(c.Request.Context(),
			storage.SetAfterTxID(afterTxIDParsed),
			storage.SetReferenceFilter(c.Query("reference")),
			storage.SetAccountFilter(c.Query("account")),
			storage.SetSourceFilter(c.Query("source")),
			storage.SetDestinationFilter(c.Query("destination")),
			storage.SetStartTime(startTimeParsed),
			storage.SetEndTime(endTimeParsed),
		)
	}

	if err != nil {
		ResponseError(c, err)
		return
	}

	respondWithCursor[core.Transaction](c, http.StatusOK, cursor)
}

func (ctl *TransactionController) PostTransaction(c *gin.Context) {
	l, _ := c.Get("ledger")

	value, ok := c.GetQuery("preview")
	preview := ok &&
		(strings.ToUpper(value) == "YES" || strings.ToUpper(value) == "TRUE" || value == "1")

	var t core.TransactionData
	if err := c.ShouldBindJSON(&t); err != nil {
		panic(err)
	}

	fn := l.(*ledger.Ledger).Commit
	if preview {
		fn = l.(*ledger.Ledger).CommitPreview
	}

	res, err := fn(c.Request.Context(), []core.TransactionData{t})
	if err != nil {
		ResponseError(c, err)
		return
	}

	status := http.StatusOK
	if preview {
		status = http.StatusNotModified
	}

	respondWithData[[]core.Transaction](c, status, res.GeneratedTransactions)
}

func (ctl *TransactionController) GetTransaction(c *gin.Context) {
	l, _ := c.Get("ledger")

	txId, err := strconv.ParseUint(c.Param("txid"), 10, 64)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	tx, err := l.(*ledger.Ledger).GetTransaction(c.Request.Context(), txId)
	if err != nil {
		ResponseError(c, err)
		return
	}

	if len(tx.Postings) == 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	respondWithData[core.Transaction](c, http.StatusOK, tx)
}

func (ctl *TransactionController) RevertTransaction(c *gin.Context) {
	l, _ := c.Get("ledger")

	txId, err := strconv.ParseUint(c.Param("txid"), 10, 64)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	tx, err := l.(*ledger.Ledger).RevertTransaction(c.Request.Context(), txId)
	if err != nil {
		ResponseError(c, err)
		return
	}

	respondWithData[*core.Transaction](c, http.StatusOK, tx)
}

func (ctl *TransactionController) PostTransactionMetadata(c *gin.Context) {
	l, _ := c.Get("ledger")

	var m core.Metadata
	if err := c.ShouldBindJSON(&m); err != nil {
		panic(err)
	}

	txId, err := strconv.ParseUint(c.Param("txid"), 10, 64)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if err := l.(*ledger.Ledger).SaveMeta(c.Request.Context(), core.MetaTargetTypeTransaction, txId, m); err != nil {
		ResponseError(c, err)
		return
	}
	respondWithNoContent(c)
}

func (ctl *TransactionController) PostTransactionsBatch(c *gin.Context) {
	l, _ := c.Get("ledger")

	var t core.Transactions
	if err := c.ShouldBindJSON(&t); err != nil {
		ResponseError(c, err)
		return
	}

	res, err := l.(*ledger.Ledger).Commit(c.Request.Context(), t.Transactions)
	if err != nil {
		ResponseError(c, err)
		return
	}

	respondWithData[[]core.Transaction](c, http.StatusOK, res.GeneratedTransactions)
}

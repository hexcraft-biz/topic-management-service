package controllers

import (
	"context"
	"net/http"

	"cloud.google.com/go/pubsub"
	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/hexcraft-biz/controller"
	"github.com/hexcraft-biz/model"
	"github.com/hexcraft-biz/topic-management-service/config"
	"github.com/hexcraft-biz/topic-management-service/models"
)

type Topics struct {
	*controller.Prototype
	Config *config.Config
}

func NewTopics(cfg *config.Config) *Topics {
	return &Topics{
		Prototype: controller.New("topics", cfg.DB),
		Config:    cfg,
	}
}

func (ctrl *Topics) NotFound() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"message": http.StatusText(http.StatusNotFound)})
	}
}

type ListTopicsParams struct {
	Limit  uint64 `form:"limit,default=20"`
	Offset uint64 `form:"offset,default=0"`
}

func (ctrl *Topics) List() gin.HandlerFunc {
	return func(c *gin.Context) {
		listParams := new(ListTopicsParams)
		if err := c.ShouldBindQuery(listParams); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": http.StatusText(http.StatusBadRequest)})
			return
		}
		pg := model.NewPagination(listParams.Offset, listParams.Limit)

		if endpoints, err := models.NewTopicsTableEngine(ctrl.DB).List(pg); err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else {
			c.AbortWithStatusJSON(http.StatusOK, endpoints)
			return
		}
	}
}

type TargetTopic struct {
	ID string `uri:"id" binding:"required"`
}

func (ctrl *Topics) GetOne() gin.HandlerFunc {
	return func(c *gin.Context) {
		var targetTopic TargetTopic
		if err := c.ShouldBindUri(&targetTopic); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}

		if entityRes, err := models.NewTopicsTableEngine(ctrl.DB).GetByID(targetTopic.ID); err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else {
			if entityRes == nil {
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"message": http.StatusText(http.StatusNotFound)})
				return
			} else {
				if absRes, absErr := entityRes.GetAbsTopic(); absErr != nil {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": absErr.Error()})
					return
				} else {
					c.AbortWithStatusJSON(http.StatusOK, absRes)
					return
				}
			}
		}
	}
}

type createTopicParams struct {
	Name string `json:"name" binding:"required,min=5,max=256"`
}

func (ctrl *Topics) Create() gin.HandlerFunc {
	return func(c *gin.Context) {
		var params createTopicParams
		if err := c.ShouldBindJSON(&params); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}

		ctx := context.Background()
		client, err := pubsub.NewClient(ctx, ctrl.Config.Env.GcpProjectID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}

		gcpTopic := client.Topic(params.Name)
		if ok, err := gcpTopic.Exists(ctx); err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else if ok == false {
			_, err := client.CreateTopic(ctx, params.Name)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
		}

		if entityRes, err := models.NewTopicsTableEngine(ctrl.DB).Insert(params.Name); err != nil {
			if err := client.Topic(params.Name).Delete(ctx); err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			if myErr, ok := err.(*mysql.MySQLError); ok && myErr.Number == 1062 {
				c.AbortWithStatusJSON(http.StatusConflict, gin.H{"message": http.StatusText(http.StatusConflict)})
				return
			} else {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
		} else {
			if absRes, absErr := entityRes.GetAbsTopic(); absErr != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": absErr.Error()})
				return
			} else {
				c.AbortWithStatusJSON(http.StatusCreated, absRes)
				return
			}
		}
	}
}

func (ctrl *Topics) Delete() gin.HandlerFunc {
	return func(c *gin.Context) {
		req, dbEngine := new(TargetTopic), models.NewTopicsTableEngine(ctrl.DB)

		if err := c.ShouldBindUri(req); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": http.StatusText(http.StatusBadRequest)})
			return
		}

		if topic, err := dbEngine.GetByID(req.ID); err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else if topic == nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"message": http.StatusText(http.StatusNotFound)})
			return
		} else {
			ctx := context.Background()
			client, err := pubsub.NewClient(ctx, ctrl.Config.Env.GcpProjectID)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			gcpTopic := client.Topic(topic.Name)
			if ok, err := gcpTopic.Exists(ctx); err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			} else if ok == true {
				if err := gcpTopic.Delete(ctx); err != nil {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
					return
				}
			}

			if _, err := dbEngine.DeleteByID(req.ID); err != nil {
				if mysqlErr, ok := err.(*mysql.MySQLError); ok && mysqlErr.Number == 1451 {
					c.AbortWithStatusJSON(http.StatusConflict, gin.H{"message": err.Error()})
					return
				} else {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
					return
				}
			} else {
				ctx := context.Background()
				ctrl.Config.Redis.FlushDB(ctx)

				c.AbortWithStatusJSON(http.StatusNoContent, gin.H{"message": http.StatusText(http.StatusNoContent)})
				return
			}
		}
	}
}

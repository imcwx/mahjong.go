package parlour

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yi-jiayu/mahjong.go/mahjong"
)

const (
	KeyPlayerID = "playerID"
	KeyRoom     = "room"
)

// newPlayerID returns opaque string containing n bytes of entropy.
func newPlayerID(n int) string {
	data := make([]byte, n)
	_, err := rand.Read(data)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func setPlayerID(c *gin.Context) {
	playerID, _ := c.Cookie("playerID")
	if playerID == "" {
		playerID = newPlayerID(16)
	}
	c.Set(KeyPlayerID, playerID)
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("playerID", playerID, 86400, "/", "", false, false)
	c.Next()
}

func getName(c *gin.Context) (string, error) {
	name := c.PostForm("name")
	if name == "" {
		return "", errors.New("name is required")
	}
	if ok, _ := regexp.MatchString("[0-9A-Za-z ]+", name); !ok {
		return "", errors.New("name is invalid")
	}
	return name, nil
}

func createRoomHandler(roomRepository RoomRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		playerID := c.GetString(KeyPlayerID)
		name, err := getName(c)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		player := Player{
			id:   playerID,
			Name: name,
		}
		room := NewRoom(player)
		err = roomRepository.Save(room)
		if err != nil {
			fmt.Printf("error saving room: %v", err)
			c.Status(http.StatusInternalServerError)
			return
		}
		c.String(http.StatusCreated, room.ID)
	}
}

func joinRoomHandler(roomRepository RoomRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		playerID := c.GetString(KeyPlayerID)
		room := c.MustGet(KeyRoom).(*Room)
		name, err := getName(c)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		player := Player{
			id:   playerID,
			Name: name,
		}
		room.WithLock(func(r *Room) {
			err = room.addPlayer(player)
			if err != nil {
				c.String(http.StatusBadRequest, err.Error())
				return
			}
			err = roomRepository.Save(r)
			if err != nil {
				fmt.Printf("error saving room: %v", err)
				c.Status(http.StatusInternalServerError)
				return
			}
		})
		if err != nil {
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func leaveRoomHandler(roomRepository RoomRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		playerID := c.GetString(KeyPlayerID)
		room := c.MustGet(KeyRoom).(*Room)
		room.WithLock(func(r *Room) {
			room.removePlayer(playerID)
			err := roomRepository.Save(r)
			if err != nil {
				fmt.Printf("error saving room: %v", err)
				c.Status(http.StatusInternalServerError)
				return
			}
		})
	}
}

func subscribeRoomHandler(roomRepository RoomRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		playerID := c.GetString(KeyPlayerID)
		room := c.MustGet(KeyRoom).(*Room)
		ch := make(chan string, 1)
		room.AddClient(playerID, ch)

		notify := c.Request.Context().Done()
		go func() {
			<-notify
			room.RemoveClient(ch)
		}()

		c.Stream(func(w io.Writer) bool {
			if update, ok := <-ch; ok {
				c.SSEvent("", update)
				return true
			}
			return false
		})
	}
}

func roomActionsHandler(roomRepository RoomRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		playerID := c.GetString(KeyPlayerID)
		room := c.MustGet(KeyRoom).(*Room)
		var action Action
		err := c.ShouldBindJSON(&action)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		room.WithLock(func(r *Room) {
			err = room.reduce(playerID, action)
			if err != nil {
				c.String(http.StatusBadRequest, err.Error())
				return
			}
			err = roomRepository.Save(r)
			if err != nil {
				fmt.Printf("error saving room: %v", err)
				c.Status(http.StatusInternalServerError)
				return
			}
		})
		if err != nil {
			return
		}
	}
}

func setConcealedHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		room := c.MustGet(KeyRoom).(*Room)
		if room.Round == nil {
			c.String(http.StatusBadRequest, "round not started")
			return
		}
		seat, err := strconv.Atoi(c.Param("seat"))
		if err != nil {
			c.String(http.StatusBadRequest, "invalid seat")
			return
		}
		if seat < 0 || 3 < seat {
			c.String(http.StatusBadRequest, "invalid seat")
			return
		}
		var tiles mahjong.TileBag
		err = c.ShouldBindJSON(&tiles)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		room.Round.Hands[seat].Concealed = tiles
	}
}

func prependWallHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		room := c.MustGet(KeyRoom).(*Room)
		if room.Round == nil {
			c.String(http.StatusBadRequest, "round not started")
			return
		}
		tile := c.PostForm("tile")
		if tile == "" {
			c.String(http.StatusBadRequest, "tile is required")
			return
		}
		room.Round.Wall = append([]mahjong.Tile{mahjong.Tile(tile)}, room.Round.Wall...)
	}
}

func configure(r *gin.Engine, roomRepository RoomRepository) {
	r.Use(setPlayerID)
	r.POST("/rooms", createRoomHandler(roomRepository))
	room := r.Group("/rooms/:roomID")
	room.Use(func(c *gin.Context) {
		roomID := c.Param("roomID")
		room, err := roomRepository.Get(roomID)
		if errors.Is(err, errNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if err != nil {
			fmt.Printf("error getting room: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Set("room", room)
		c.Next()
	})
	{
		room.POST("/players", joinRoomHandler(roomRepository))
		room.DELETE("/players", leaveRoomHandler(roomRepository))
		room.GET("/live", subscribeRoomHandler(roomRepository))
		room.POST("/actions", roomActionsHandler(roomRepository))
		if gin.IsDebugging() {
			room.PUT("/round/hands/:seat/concealed", setConcealedHandler())
			room.POST("/round/wall", prependWallHandler())
		}
	}
}

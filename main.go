package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/thedevsaddam/renderer"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

var rnd *renderer.Render
var collection *mongo.Collection
var ctx = context.TODO()

const (
	hostName       string = "localhost:27017"
	dbName         string = "my_todo"
	collectionName string = "todo"
	port           string = ":9000"
)

type (
	todoModel struct {
		ID        bson.ObjectId `bson:"_id,omitempty"`
		Title     string        `bson:"title"`
		Completed bool          `bson:"completed"`
		CreatedAt time.Time     `bson:"createdAt"`
	}
	todo struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Completed bool      `json:"completed"`
		CreatedAt time.Time `json:"createdAt"`
	}
)

func init() {
	clientOptions := options.Client().ApplyURI("mongodb://localhost:27017")
	client, err := mongo.Connect(ctx, clientOptions)

	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	collection = (client.Database(dbName).Collection(collectionName))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	err := rnd.Template(w, http.StatusOK, []string{"static/home.tpl"}, nil)
	checkErr(err)
}

func fetchTodos(w http.ResponseWriter, r *http.Request) {
	todos := []todoModel{}

	fetch, err := collection.Find(ctx, bson.M{})

	if err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to fetch todos",
			"error":   err,
		})
		return
	}
	defer fetch.Close(ctx)

	for fetch.Next(ctx) {
		var t todoModel
		err := fetch.Decode(&t)
		if err != nil {
			rnd.JSON(w, http.StatusProcessing, renderer.M{
				"message": "Failed to fetch todos",
				"error":   err,
			})
			return
		}
		todos = append(todos, t)
	}

	if err := fetch.Err(); err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to fetch todos",
			"error":   err,
		})
		return
	}

	todoList := []todo{}

	for _, t := range todos {
		todoList = append(todoList, todo{
			ID:        t.ID.Hex(),
			Title:     t.Title,
			Completed: t.Completed,
			CreatedAt: t.CreatedAt,
		})
	}
	rnd.JSON(w, http.StatusOK, renderer.M{
		"data": todoList,
	})
}

func createTodo(w http.ResponseWriter, r *http.Request) {
	var t todo

	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		rnd.JSON(w, http.StatusProcessing, err)
		return
	}

	if t.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The title is required",
		})
		return
	}

	tm := todoModel{
		ID:        bson.NewObjectId(),
		Title:     t.Title,
		Completed: false,
		CreatedAt: time.Now(),
	}

	if _, err := collection.InsertOne(ctx, &tm); err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to save todo",
			"error":   err,
		})
		return
	}

	rnd.JSON(w, http.StatusCreated, renderer.M{
		"message": "Todo created successfully",
		"todo_id": tm.ID.Hex(),
	})
}

func deleteTodo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

    if !bson.IsObjectIdHex(id) {
        rnd.JSON(w, http.StatusBadRequest, renderer.M{
            "message": "Invalid todo ID",
        })
        return
    }

    objID := bson.ObjectIdHex(id)
    filter := bson.M{"_id": objID}

    if _, err := collection.DeleteOne(ctx, filter, nil); err != nil {
        rnd.JSON(w, http.StatusProcessing, renderer.M{
            "message": "Failed to delete todo",
            "error":   err,
        })
        return
    }

    rnd.JSON(w, http.StatusOK, renderer.M{
        "message": "Todo deleted successfully",
    })
}

func updateTodo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The todo ID is required",
		})
		return
	}

	var t todo

	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		rnd.JSON(w, http.StatusProcessing, err)
		return
	}

	if t.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The title is required",
		})
		return
	}

	filter := bson.M{"_id": bson.ObjectIdHex(id)}
	update := bson.M{
		"$set": bson.M{
			"title":     t.Title,
			"completed": t.Completed,
		},
	}

	_, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to update todo",
			"error":   err,
		})
		return
	}

	rnd.JSON(w, http.StatusOK, renderer.M{
		"message": "Todo successfully updated",
	})
}

func main() {
	rnd = renderer.New()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Get("/", homeHandler)

	r.Mount("/todo", todoHandlers())

	srv := http.Server{
		Addr:         port,
		Handler:      r,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Println("Listening on port", port)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("Listen: %s\n", err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("Server gracefully stopped!")
	os.Exit(0)
}

func todoHandlers() http.Handler {
	rg := chi.NewRouter()
	rg.Group(func(r chi.Router) {
		r.Get("/", fetchTodos)
		r.Post("/", createTodo)
		r.Put("/{id}", updateTodo)
		r.Delete("/{id}", deleteTodo)
	})
	return rg
}

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

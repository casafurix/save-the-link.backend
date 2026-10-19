package authservice

import (
	"strings"

	mongomodels "github.com/smil-thakur/save-the-link/internals/mongo_models"
	"github.com/smil-thakur/save-the-link/internals/repository"
)

type AuthService struct {
	userRepository *repository.UserRepository
}

func NewAuthService(userRepository *repository.UserRepository) *AuthService {
	return &AuthService{
		userRepository: userRepository,
	}
}

func (s *AuthService) RegisterNewUser(username string, email string, password string) (string, error) {
	id, err := s.userRepository.RegisterUserToDatabase(strings.TrimSpace(username), normalizeEmail(email), password)

	if err != nil {
		return "", err
	}

	return id, nil
}

func (s *AuthService) GetUserById(id string) (*mongomodels.UserMongo, error) {
	user, err := s.userRepository.FindUserById(id)

	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *AuthService) LoginUser(email string, password string) (*mongomodels.UserMongo, error) {

	user, err := s.userRepository.LoginUser(strings.TrimSpace(email), password)

	if err != nil {
		return nil, err
	}

	return user, nil

}

func (s *AuthService) DeleteAccount(userId string) error {
	return s.userRepository.DeleteUser(userId)
}

// SearchUsers powers the collaborator autocomplete — requires at least 2
// characters so it can't be used to enumerate the whole user base one
// keystroke at a time.
func (s *AuthService) SearchUsers(query string, requesterId string) ([]mongomodels.UserMongo, error) {
	query = normalizeEmail(query)
	if len(query) < 2 {
		return []mongomodels.UserMongo{}, nil
	}

	return s.userRepository.SearchUsersByEmail(query, requesterId, 6)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

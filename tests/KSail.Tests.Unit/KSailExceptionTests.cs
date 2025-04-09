namespace KSail.Tests.Unit;


public class KSailExceptionTests
{

  [Fact]
  public void KSailException_DefaultConstructor_ShouldCreateInstance()
  {
    // Act
    var exception = new KSailException();

    // Assert
    Assert.NotNull(exception);
    _ = Assert.IsType<KSailException>(exception);
  }


  [Fact]
  public void KSailException_ConstructorWithMessage_ShouldSetMessage()
  {
    // Arrange
    string message = "Test message";

    // Act
    var exception = new KSailException(message);

    // Assert
    Assert.NotNull(exception);
    Assert.Equal(message, exception.Message);
  }


  [Fact]
  public void KSailException_ConstructorWithMessageAndInnerException_ShouldSetMessageAndInnerException()
  {
    // Arrange
    string message = "Test message";
    var innerException = new KSailException("Inner exception");

    // Act
    var exception = new KSailException(message, innerException);

    // Assert
    Assert.NotNull(exception);
    Assert.Equal(message, exception.Message);
    Assert.Equal(innerException, exception.InnerException);
  }
}

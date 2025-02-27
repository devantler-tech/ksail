namespace KSail;


[Serializable]
internal class KSailException : Exception
{

  public KSailException()
  {
  }



  public KSailException(string? message) : base(message)
  {
  }




  public KSailException(string? message, Exception? innerException) : base(message, innerException)
  {
  }
}
